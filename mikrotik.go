package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DHCPLease represents a DHCP lease from Mikrotik
type DHCPLease struct {
	ID               string `json:".id"`
	ActiveAddress    string `json:"active-address"`
	ActiveClientID   string `json:"active-client-id"`
	ActiveMacAddress string `json:"active-mac-address"`
	ActiveServer     string `json:"active-server"`
	Address          string `json:"address"`
	AddressLists     string `json:"address-lists"`
	Blocked          string `json:"blocked"`
	ClassID          string `json:"class-id"`
	ClientID         string `json:"client-id"`
	DHCPOption       string `json:"dhcp-option"`
	Disabled         string `json:"disabled"`
	Dynamic          string `json:"dynamic"`
	ExpiresAfter     string `json:"expires-after"`
	HostName         string `json:"host-name"`
	LastSeen         string `json:"last-seen"`
	MacAddress       string `json:"mac-address"`
	Radius           string `json:"radius"`
	Server           string `json:"server"`
	Status           string `json:"status"`
}

// getMikrotikDHCPLeases retrieves all DHCP leases from Mikrotik router
func getMikrotikDHCPLeases() ([]DHCPLease, error) {
	url := fmt.Sprintf("http://%s:%s/rest/ip/dhcp-server/lease", mikrotikIP, mikrotikPort)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(mikrotikUsername, mikrotikPassword)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("mikrotik API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var leases []DHCPLease
	if err := json.Unmarshal(body, &leases); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	return leases, nil
}

// findIPByMACAddress finds the IP address for a given MAC address from DHCP leases
func findIPByMACAddress(macAddress string) (string, error) {
	leases, err := getMikrotikDHCPLeases()
	if err != nil {
		return "", fmt.Errorf("failed to get DHCP leases: %w", err)
	}

	// Normalize MAC address format (convert to uppercase and ensure consistent format)
	normalizedMAC := strings.ToUpper(strings.ReplaceAll(macAddress, "-", ":"))

	for _, lease := range leases {
		leaseMac := strings.ToUpper(strings.ReplaceAll(lease.MacAddress, "-", ":"))
		activeMac := strings.ToUpper(strings.ReplaceAll(lease.ActiveMacAddress, "-", ":"))

		if leaseMac == normalizedMAC || activeMac == normalizedMAC {
			// Prefer active-address if available, otherwise use address
			if lease.ActiveAddress != "" {
				return lease.ActiveAddress, nil
			}
			if lease.Address != "" {
				return lease.Address, nil
			}
		}
	}

	return "", fmt.Errorf("no DHCP lease found for MAC address: %s", macAddress)
}
