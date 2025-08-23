package main

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

// TalosConfig represents the structure for Talos machine configuration
type TalosConfig struct {
	Version string `yaml:"version"`
	Debug   bool   `yaml:"debug"`
	Persist bool   `yaml:"persist"`
	Machine struct {
		Type    string `yaml:"type"`
		Token   string `yaml:"token"`
		Network struct {
			Hostname string `yaml:"hostname"`
		} `yaml:"network"`
	} `yaml:"machine"`
	Cluster struct {
		ID           string `yaml:"id"`
		Secret       string `yaml:"secret"`
		ControlPlane struct {
			Endpoint string `yaml:"endpoint"`
		} `yaml:"controlPlane"`
		ClusterName string `yaml:"clusterName"`
		Network     struct {
			DNSDomain      string   `yaml:"dnsDomain"`
			PodSubnets     []string `yaml:"podSubnets"`
			ServiceSubnets []string `yaml:"serviceSubnets"`
		} `yaml:"network"`
		Token string `yaml:"token"`
	} `yaml:"cluster"`
}

func generateTalosConfig(templatePath string, vmName string, vmIP string, role string, nodeName string, vmTemplate string, cpuModel string, memory int, suffix string, cpuCores int, disk string) (string, error) {
	templateContent, err := os.ReadFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("failed to read Talos template file %s: %v", templatePath, err)
	}

	config := string(templateContent)
	config = strings.ReplaceAll(config, "{role}", role)
	config = strings.ReplaceAll(config, "{vm_name}", vmName)
	config = strings.ReplaceAll(config, "{node}", nodeName)
	config = strings.ReplaceAll(config, "{vm_template}", vmTemplate)
	config = strings.ReplaceAll(config, "{cpu}", cpuModel)
	config = strings.ReplaceAll(config, "{memory}", fmt.Sprintf("%d", memory))
	config = strings.ReplaceAll(config, "{suffix}", suffix)
	config = strings.ReplaceAll(config, "{cpu_cores}", fmt.Sprintf("%d", cpuCores))
	config = strings.ReplaceAll(config, "{disk}", disk)

	return config, nil
}

func registerTalosNode(vmIP string, talosConfig string, controlPlaneEndpoint string) error {
	configFile := fmt.Sprintf("/tmp/talos-config-%s.yaml", strings.ReplaceAll(vmIP, ".", "-"))
	if err := os.WriteFile(configFile, []byte(talosConfig), 0600); err != nil {
		return fmt.Errorf("failed to write Talos config file: %v", err)
	}

	cmd := exec.Command("talosctl", "apply-config", "--insecure", "--nodes", vmIP, "--file", configFile)
	cmd.Env = os.Environ()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		logger.Error("talosctl failed: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
		return fmt.Errorf("failed to apply Talos config: %v", err)
	}

	logger.Info("Applied Talos config to node: %s", vmIP)
	return nil
}

func waitForTalosNode(vmIP string) error {
	for attempt := 1; attempt <= 30; attempt++ {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:50000", vmIP), 5*time.Second)
		if err != nil {
			if attempt == 30 {
				return fmt.Errorf("Talos node not ready after 30 attempts: %v", err)
			}
			time.Sleep(10 * time.Second)
			continue
		}
		conn.Close()
		return nil
	}
	return fmt.Errorf("Talos node not ready after 30 attempts")
}
