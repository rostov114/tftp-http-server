package main

import (
        "flag"
        "fmt"
        "io"
        "log"
        "net"
        "net/http"
        "net/url"
        "strings"
        "time"

        systemd "github.com/coreos/go-systemd/v22/daemon"
        "github.com/pin/tftp/v3"
)

const (
        httpBaseURLDefault  = "http://tftp.local"
        tftpTimeoutDefault  = 5 * time.Second
        tftpBindAddrDefault = ":69"
)

var globalState = struct {
        httpBaseURL string
        httpClient  *http.Client
}{
        httpBaseURL: httpBaseURLDefault,
        httpClient:  nil,
}

func tftpReadHandler(filename string, rf io.ReaderFrom) error {
        outgoing, ok := rf.(tftp.OutgoingTransfer)
        if !ok {
                return fmt.Errorf("TFTP transfer does not implement OutgoingTransfer")
        }

        raddr := outgoing.RemoteAddr()

        log.Printf(
                "INFO: New TFTP request (%s) from %s:%d",
                filename,
                raddr.IP.String(),
                raddr.Port,
        )

        filename = strings.TrimLeft(filename, "/")

        if filename == "" {
                log.Printf("ERR: empty TFTP filename")
                return fmt.Errorf("empty TFTP filename")
        }

        uri := strings.TrimRight(globalState.httpBaseURL, "/") + "/" + filename

        log.Printf(
                "INFO: HTTP proxy request: %s",
                uri,
        )

        req, err := http.NewRequest(http.MethodGet, uri, nil)
        if err != nil {
                log.Printf(
                        "ERR: HTTP request setup failed: %v",
                        err,
                )
                return err
        }

        req.Header.Set(
                "X-TFTP-IP",
                raddr.IP.String(),
        )

        req.Header.Set(
                "X-TFTP-Port",
                fmt.Sprintf("%d", raddr.Port),
        )

        req.Header.Set(
                "X-TFTP-File",
                filename,
        )

        resp, err := globalState.httpClient.Do(req)
        if err != nil {
                log.Printf(
                        "ERR: HTTP request failed: %v",
                        err,
                )
                return err
        }

        defer resp.Body.Close()

        switch resp.StatusCode {
        case http.StatusOK:

        case http.StatusNotFound:
                log.Printf(
                        "INFO: HTTP file not found: %s",
                        resp.Status,
                )

                return fmt.Errorf("file not found")

        default:
                log.Printf(
                        "ERR: HTTP request returned status %s",
                        resp.Status,
                )

                return fmt.Errorf(
                        "HTTP request error: %s",
                        resp.Status,
                )
        }

        if resp.ContentLength >= 0 {
                outgoing.SetSize(resp.ContentLength)
        }


        _, err = rf.ReadFrom(resp.Body)
        if err != nil {
                log.Printf(
                        "ERR: ReadFrom failed: %v",
                        err,
                )

                return err
        }

        log.Printf(
                "INFO: TFTP transfer completed: %s",
                filename,
        )

        return nil
}

func parseBaseURL(baseURL string) string {
        u, err := url.ParseRequestURI(baseURL)
        if err != nil {
                log.Fatalf(
                        "FATAL: invalid base URL: %v",
                        err,
                )
        }

        if u.Scheme == "" {
                log.Fatal(
                        "FATAL: invalid base URL: no scheme found",
                )
        }

        if u.Host == "" {
                log.Fatal(
                        "FATAL: invalid base URL: no host found",
                )
        }

        return strings.TrimRight(u.String(), "/")
}

func notifySystemd() {
        sent, err := systemd.SdNotify(
                true,
                "READY=1",
        )

        if err != nil {
                log.Printf(
                        "WARN: Unable to send systemd daemon successful start message: %v",
                        err,
                )

                return
        }

        if sent {
                log.Printf(
                        "DEBUG: Systemd was notified.",
                )
        } else {
                log.Printf(
                        "DEBUG: Systemd notifications are not supported.",
                )
        }
}

func main() {
        httpBaseURLPtr := flag.String(
                "http-base-url",
                httpBaseURLDefault,
                "HTTP provisioning server URL",
        )

        tftpTimeoutPtr := flag.Duration(
                "tftp-timeout",
                tftpTimeoutDefault,
                "TFTP timeout",
        )

        bindAddrPtr := flag.String(
                "tftp-bind-address",
                tftpBindAddrDefault,
                "TFTP address to bind to",
        )

        flag.Parse()

        globalState.httpBaseURL = parseBaseURL(
                *httpBaseURLPtr,
        )

        globalState.httpClient = &http.Client{}

        server := tftp.NewServer(
                tftpReadHandler,
                nil,
        )

        server.SetTimeout(
                *tftpTimeoutPtr,
        )

        conn, err := net.ListenPacket(
                "udp",
                *bindAddrPtr,
        )

        if err != nil {
                log.Fatalf(
                        "FATAL: unable to bind TFTP server to %s: %v",
                        *bindAddrPtr,
                        err,
                )
        }

        defer conn.Close()

        log.Printf(
                "INFO: Listening TFTP requests on: %s",
                *bindAddrPtr,
        )

        log.Printf(
                "INFO: Provisioning HTTP server: %s",
                globalState.httpBaseURL,
        )

        notifySystemd()

        if err := server.Serve(conn); err != nil {
                log.Fatalf(
                        "FATAL: tftp server: %v",
                        err,
                )
        }
}
