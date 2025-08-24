package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func getNextID() (int, error) {
	endpoint := fmt.Sprintf("%s/cluster/nextid", proxmoxBaseAddr)
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Add("Authorization", "PVEAPIToken="+proxmoxToken)

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	logger.Debug("getNextID raw response: %s", string(body))
	var result struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}
	nextID, err := strconv.Atoi(result.Data)
	if err != nil {
		return 0, err
	}
	logger.Info("Obtained next VM id: %d", nextID)
	return nextID, nil
}

func cloneVM(node string, templateID int, newid int, name string) (string, error) {
	endpoint := fmt.Sprintf("%s/nodes/%s/qemu/%d/clone", proxmoxBaseAddr, node, templateID)
	data := url.Values{}
	data.Set("newid", strconv.Itoa(newid))
	data.Set("name", name)
	data.Set("full", "1")
	data.Set("format", "raw")

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Add("Authorization", "PVEAPIToken="+proxmoxToken)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	logger.Debug("cloneVM raw response: %s", string(body))
	var result struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	logger.Info("Clone task created successfully: %s", result.Data)
	return result.Data, nil
}

func trackTask(node string, upid string) error {
	statusEndpoint := fmt.Sprintf("%s/nodes/%s/tasks/%s/status", proxmoxBaseAddr, node, upid)
	for {
		req, err := http.NewRequest("GET", statusEndpoint, nil)
		if err != nil {
			return err
		}
		req.Header.Add("Authorization", "PVEAPIToken="+proxmoxToken)

		resp, err := httpClient.Do(req)
		if err != nil {
			return err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		logger.Debug("trackTask raw response for %s: %s", upid, string(body))
		var raw struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(body, &raw); err != nil {
			return err
		}

		var statusObj struct {
			Status     string `json:"status"`
			ExitStatus string `json:"exitstatus"`
		}
		if len(raw.Data) > 0 && raw.Data[0] == '{' {
			if err := json.Unmarshal(raw.Data, &statusObj); err != nil {
				return err
			}
		} else if len(raw.Data) > 0 && raw.Data[0] == '[' {
			var statusArr []struct {
				Status     string `json:"status"`
				ExitStatus string `json:"exitstatus"`
			}
			if err := json.Unmarshal(raw.Data, &statusArr); err != nil {
				return err
			}
			if len(statusArr) == 0 {
				return fmt.Errorf("empty task status array for %s", upid)
			}
			statusObj = statusArr[0]
		} else {
			return fmt.Errorf("unexpected task status format for %s", upid)
		}

		if statusObj.Status == "running" {
			time.Sleep(2 * time.Second)
			continue
		}
		if statusObj.Status == "stopped" {
			if statusObj.ExitStatus == "OK" {
				logger.Info("Task %s completed successfully", upid)
				return nil
			}
			return fmt.Errorf("task %s failed with exit status: %s", upid, statusObj.ExitStatus)
		}
		return fmt.Errorf("unknown task status for %s", upid)
	}
}

func getVMConfig(node string, vmid int) (map[string]interface{}, error) {
	endpoint := fmt.Sprintf("%s/nodes/%s/qemu/%d/config", proxmoxBaseAddr, node, vmid)
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", "PVEAPIToken="+proxmoxToken)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	logger.Debug("getVMConfig raw response: %s", string(body))
	var result struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

func configureVM(node string, vmid int, cores int, memory int, cpuModel string, numa string, phyCores string, htCores string, phyOnly bool, htOnly bool, nodeConfig *NodeConfig) (string, error) {
	currentConfig, err := getVMConfig(node, vmid)
	if err != nil {
		logger.Error("Failed to get current VM config: %s", err.Error())
	}

	endpoint := fmt.Sprintf("%s/nodes/%s/qemu/%d/config", proxmoxBaseAddr, node, vmid)
	data := url.Values{}

	if cpuModel != "" {
		data.Set("cpu", cpuModel)
	} else {
		data.Set("cpu", "x86-64-v3")
	}

	data.Set("cores", strconv.Itoa(cores))
	data.Set("memory", strconv.Itoa(memory))

	if nodeConfig.Hugepages {
		data.Set("hugepages", "2")
		logger.Info("Setting hugepages=2 for VM")
	}

	data.Set("numa", "1")

	if currentConfig != nil {
		if virtio0, ok := currentConfig["virtio0"].(string); ok && virtio0 != "" {
			if !strings.Contains(virtio0, "aio=") {
				virtio0 += ",aio=native"
				data.Set("virtio0", virtio0)
				logger.Info("Setting virtio0 with aio=native: %s", virtio0)
			}
		}

		if net0, ok := currentConfig["net0"].(string); ok && net0 != "" {
			if !strings.Contains(net0, "queues=") {
				net0 += ",queues=2"
				data.Set("net0", net0)
				logger.Info("Setting net0 with queues=2: %s", net0)
			}
		}
	}

	if nodeConfig == nil {
		logger.Error("Failed to find node configuration for node: %s", node)
		return "", fmt.Errorf("node configuration not found for %s", node)
	}

	var numaNode *NumaNode

	if numa != "" {
		numaID, err := strconv.Atoi(numa)
		if err != nil {
			logger.Error("Invalid NUMA node ID: %s", numa)
			return "", err
		}

		for i, node := range nodeConfig.NUMA {
			if node.ID == numaID {
				numaNode = &nodeConfig.NUMA[i]
				break
			}
		}

		if numaNode == nil {
			logger.Error("NUMA node %d not found", numaID)
			return "", fmt.Errorf("NUMA node %d not found", numaID)
		}
	} else {
		numaNode, err = selectRandomNumaNode(node)
		if err != nil {
			logger.Error("Failed to select NUMA node: %s", err.Error())
			return "", err
		}
		logger.Info("Auto-selected NUMA node ID: %d", numaNode.ID)
	}

	var selectedCores string
	if phyCores != "" || htCores != "" {
		var coreParts []string
		if phyCores != "" {
			coreParts = append(coreParts, phyCores)
		}
		if htCores != "" {
			coreParts = append(coreParts, htCores)
		}
		selectedCores = strings.Join(coreParts, ",")

		totalCores := countCoresFromRange(phyCores) + countCoresFromRange(htCores)
		if totalCores != cores {
			logger.Info("Total specified cores (%d) doesn't match VM template cores (%d)", totalCores, cores)
		}
	} else {
		selectedCores = autoSelectCores(numaNode, phyOnly, htOnly)
	}

	if selectedCores != "" {
		data.Set("affinity", selectedCores)
		logger.Info("Setting CPU affinity: %s", selectedCores)
	}

	data.Set("sockets", "1")
	data.Set("cores", strconv.Itoa(cores))
	data.Set("numa", "1")

	virtualCoresList := fmt.Sprintf("0-%d", cores-1)

	numaConfig := fmt.Sprintf("cpus=%s,memory=%d,hostnodes=%d,policy=bind", virtualCoresList, memory, numaNode.ID)
	data.Set("numa0", numaConfig)
	logger.Info("Setting guest NUMA topology: numa0=%s", numaConfig)

	logURLValues("VM Configure", data)

	encodedData := data.Encode()
	req, err := http.NewRequest("POST", endpoint, strings.NewReader(encodedData))
	if err != nil {
		return "", err
	}
	req.Header.Add("Authorization", "PVEAPIToken="+proxmoxToken)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	logger.Debug("configureVM raw response: %s", string(body))
	var result struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	logger.Info("Configure VM task created successfully: %s", result.Data)
	return result.Data, nil
}

func resizeDisk(node string, vmid int, diskSize int) (string, error) {
	endpoint := fmt.Sprintf("%s/nodes/%s/qemu/%d/resize", proxmoxBaseAddr, node, vmid)
	data := url.Values{}
	data.Set("disk", "virtio0")
	data.Set("size", fmt.Sprintf("%dG", diskSize))

	req, err := http.NewRequest("PUT", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Add("Authorization", "PVEAPIToken="+proxmoxToken)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	logger.Debug("resizeDisk raw response: %s", string(body))
	var result struct {
		Data *string `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	if result.Data == nil || *result.Data == "" {
		logger.Info("Resize disk completed successfully (synchronous operation)")
		return "", nil
	}
	logger.Info("Resize disk task created successfully: %s", *result.Data)
	return *result.Data, nil
}

func startVM(node string, vmid int) (string, error) {
	endpoint := fmt.Sprintf("%s/nodes/%s/qemu/%d/status/start", proxmoxBaseAddr, node, vmid)
	req, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Add("Authorization", "PVEAPIToken="+proxmoxToken)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	logger.Debug("startVM raw response: %s", string(body))
	var result struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	logger.Info("Start VM task created successfully: %s", result.Data)
	return result.Data, nil
}

func stopVM(node string, vmid int, method string) (string, error) {
	var endpoint string
	if method == "stop" {
		endpoint = fmt.Sprintf("%s/nodes/%s/qemu/%d/status/stop", proxmoxBaseAddr, node, vmid)
	} else {
		endpoint = fmt.Sprintf("%s/nodes/%s/qemu/%d/status/shutdown", proxmoxBaseAddr, node, vmid)
	}
	req, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Add("Authorization", "PVEAPIToken="+proxmoxToken)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	logger.Debug("stopVM raw response: %s", string(body))
	var result struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	logger.Info("Stop VM task created successfully: %s", result.Data)
	return result.Data, nil
}

func deleteVM(node string, vmid int) (string, error) {
	endpoint := fmt.Sprintf("%s/nodes/%s/qemu/%d", proxmoxBaseAddr, node, vmid)
	req, err := http.NewRequest("DELETE", endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Add("Authorization", "PVEAPIToken="+proxmoxToken)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	logger.Debug("deleteVM raw response: %s", string(body))
	var result struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	logger.Info("Delete VM task created successfully: %s", result.Data)
	return result.Data, nil
}

func resetVM(node string, vmid int) (string, error) {
	endpoint := fmt.Sprintf("%s/nodes/%s/qemu/%d/status/reset", proxmoxBaseAddr, node, vmid)
	req, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Add("Authorization", "PVEAPIToken="+proxmoxToken)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	logger.Debug("resetVM raw response: %s", string(body))
	var result struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	logger.Info("Reset VM task created successfully: %s", result.Data)
	return result.Data, nil
}

func getNodeConfigByName(nodeName string) *NodeConfig {
	for _, node := range config.Nodes {
		if node.Name == nodeName {
			return &node
		}
	}
	return nil
}

func countCoresFromRange(coreRange string) int {
	if coreRange == "" {
		return 0
	}

	count := 0
	parts := strings.Split(coreRange, ",")

	for _, part := range parts {
		if strings.Contains(part, "-") {
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) != 2 {
				logger.Error("Invalid core range format: %s", part)
				continue
			}

			start, err := strconv.Atoi(rangeParts[0])
			if err != nil {
				logger.Error("Invalid core range start: %s", rangeParts[0])
				continue
			}

			end, err := strconv.Atoi(rangeParts[1])
			if err != nil {
				logger.Error("Invalid core range end: %s", rangeParts[1])
				continue
			}

			count += (end - start + 1)
		} else {
			count++
		}
	}

	return count
}

func selectRandomNumaNode(nodeName string) (*NumaNode, error) {
	nodeConfig := getNodeConfigByName(nodeName)
	if nodeConfig == nil {
		return nil, fmt.Errorf("node configuration not found for %s", nodeName)
	}

	if len(nodeConfig.NUMA) == 0 {
		return nil, fmt.Errorf("no NUMA nodes defined for node %s", nodeName)
	}

	numaIndex := rand.Intn(len(nodeConfig.NUMA))
	return &nodeConfig.NUMA[numaIndex], nil
}

func findVMByName(node string, vmName string) (int, error) {
	endpoint := fmt.Sprintf("%s/nodes/%s/qemu", proxmoxBaseAddr, node)
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Add("Authorization", "PVEAPIToken="+proxmoxToken)
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	logger.Debug("findVMByName raw response: %s", string(body))
	var result struct {
		Data []struct {
			VMID int    `json:"vmid"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}
	for _, vm := range result.Data {
		if vm.Name == vmName {
			return vm.VMID, nil
		}
	}
	return 0, fmt.Errorf("VM with name %s not found on node %s", vmName, node)
}

// getVMIPAddressFromGuestAgent gets VM IP address using qemu-guest-agent
func getVMIPAddressFromGuestAgent(node string, vmid int) (string, error) {
	endpoint := fmt.Sprintf("%s/nodes/%s/qemu/%d/agent/network-get-interfaces", proxmoxBaseAddr, node, vmid)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Add("Authorization", "PVEAPIToken="+proxmoxToken)

	// Retry logic for getting IP address as guest agent might need time to start
	maxRetries := 100
	retryDelay := 3 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		resp, err := httpClient.Do(req)
		if err != nil {
			if attempt == maxRetries {
				return "", fmt.Errorf("failed to connect to guest agent after %d attempts: %w", maxRetries, err)
			}
			logger.Info("Attempt %d/%d: Guest agent not ready, retrying in %v: %v", attempt, maxRetries, retryDelay, err)
			time.Sleep(retryDelay)
			continue
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			if attempt == maxRetries {
				return "", fmt.Errorf("failed to read guest agent response after %d attempts: %w", maxRetries, err)
			}
			logger.Info("Attempt %d/%d: Failed to read response, retrying in %v: %v", attempt, maxRetries, retryDelay, err)
			time.Sleep(retryDelay)
			continue
		}

		logger.Debug("Guest agent network interfaces response: %s", string(body))

		var result struct {
			Data struct {
				Result []struct {
					Name        string `json:"name"`
					IPAddresses []struct {
						IPAddress     string `json:"ip-address"`
						IPAddressType string `json:"ip-address-type"`
					} `json:"ip-addresses"`
				} `json:"result"`
			} `json:"data"`
		}

		if err := json.Unmarshal(body, &result); err != nil {
			if attempt == maxRetries {
				return "", fmt.Errorf("failed to parse guest agent response after %d attempts: %w", maxRetries, err)
			}
			logger.Info("Attempt %d/%d: Failed to parse response, retrying in %v: %v", attempt, maxRetries, retryDelay, err)
			time.Sleep(retryDelay)
			continue
		}

		// Look for IPv4 address on the specified interface
		targetInterface := talosVMInterface
		if targetInterface == "" {
			targetInterface = "eth0" // fallback default
		}

		for _, iface := range result.Data.Result {
			// Skip loopback interface
			if iface.Name == "lo" {
				continue
			}

			// Check if this is the target interface
			if iface.Name == targetInterface {
				for _, addr := range iface.IPAddresses {
					if addr.IPAddressType == "ipv4" && !strings.HasPrefix(addr.IPAddress, "127.") {
						logger.Info("Found IP address from guest agent on interface %s: %s", targetInterface, addr.IPAddress)
						return addr.IPAddress, nil
					}
				}
			}
		}

		// If target interface not found, try any non-loopback interface as fallback
		logger.Debug("Target interface %s not found, trying any available interface", targetInterface)
		for _, iface := range result.Data.Result {
			if iface.Name == "lo" {
				continue // Skip loopback interface
			}
			for _, addr := range iface.IPAddresses {
				if addr.IPAddressType == "ipv4" && !strings.HasPrefix(addr.IPAddress, "127.") {
					logger.Info("Found IP address from guest agent on fallback interface %s: %s", iface.Name, addr.IPAddress)
					return addr.IPAddress, nil
				}
			}
		}

		if attempt == maxRetries {
			return "", fmt.Errorf("no valid IPv4 address found in guest agent response after %d attempts", maxRetries)
		}

		logger.Info("Attempt %d/%d: No valid IP found, retrying in %v", attempt, maxRetries, retryDelay)
		time.Sleep(retryDelay)
	}

	return "", fmt.Errorf("failed to get VM IP address from guest agent after %d attempts", maxRetries)
}

// getVMIPAddress gets VM IP address using qemu-guest-agent
func getVMIPAddress(node string, vmid int) (string, error) {
	logger.Info("Getting VM IP using qemu-guest-agent...")
	return getVMIPAddressFromGuestAgent(node, vmid)
}
