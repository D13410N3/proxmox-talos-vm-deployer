package main

import (
	"fmt"
	"math/rand"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
)

const letterBytes = "abcdefghijklmnopqrstuvwxyz0123456789"

func generateRandomString(n int) string {
	rand.Seed(time.Now().UnixNano())
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}

func reportError(err error) {
	sentry.CaptureException(err)
}

func selectWeightedNode(nodes []NodeConfig) *NodeConfig {
	totalWeight := 0
	for _, node := range nodes {
		totalWeight += node.Weight
	}
	randNum := rand.Intn(totalWeight)
	for i, node := range nodes {
		if randNum < node.Weight {
			return &nodes[i]
		}
		randNum -= node.Weight
	}
	return nil
}

func logURLValues(prefix string, data url.Values) {
	logger.Info("%s - Full encoded data: %s", prefix, data.Encode())
	logger.Info("%s - Individual parameters:", prefix)
	for key, values := range data {
		for _, value := range values {
			logger.Info("%s   %s: %s", prefix, key, value)
		}
	}
}

func parseCoreRange(coreRange string) []int {
	if coreRange == "" {
		return nil
	}

	var cores []int
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

			for i := start; i <= end; i++ {
				cores = append(cores, i)
			}
		} else {
			coreID, err := strconv.Atoi(part)
			if err != nil {
				logger.Error("Invalid core ID: %s", part)
				continue
			}
			cores = append(cores, coreID)
		}
	}

	return cores
}

func formatCoreRange(cores []int) string {
	if len(cores) == 0 {
		return ""
	}

	sort.Ints(cores)

	var ranges []string
	start := cores[0]
	end := start

	for i := 1; i < len(cores); i++ {
		if cores[i] == end+1 {
			end = cores[i]
		} else {
			if start == end {
				ranges = append(ranges, strconv.Itoa(start))
			} else {
				ranges = append(ranges, fmt.Sprintf("%d-%d", start, end))
			}
			start = cores[i]
			end = start
		}
	}

	if start == end {
		ranges = append(ranges, strconv.Itoa(start))
	} else {
		ranges = append(ranges, fmt.Sprintf("%d-%d", start, end))
	}

	return strings.Join(ranges, ",")
}

func parseRangeComponent(rangeStr string) (int, int, error) {
	if rangeStr == "" {
		return 0, 0, fmt.Errorf("empty range string")
	}

	parts := strings.Split(rangeStr, ",")
	rangeComponent := parts[0]

	if strings.Contains(rangeComponent, "-") {
		rangeParts := strings.Split(rangeComponent, "-")
		if len(rangeParts) != 2 {
			return 0, 0, fmt.Errorf("invalid range format: %s", rangeComponent)
		}

		start, err := strconv.Atoi(rangeParts[0])
		if err != nil {
			return 0, 0, fmt.Errorf("invalid start value: %s", rangeParts[0])
		}

		end, err := strconv.Atoi(rangeParts[1])
		if err != nil {
			return 0, 0, fmt.Errorf("invalid end value: %s", rangeParts[1])
		}

		return start, end, nil
	} else {
		// Single value
		value, err := strconv.Atoi(rangeComponent)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid value: %s", rangeComponent)
		}

		return value, value, nil
	}
}

func autoSelectCores(numaNode *NumaNode, phyOnly bool, htOnly bool) string {
	if numaNode == nil {
		logger.Error("NUMA node is nil")
		return ""
	}

	phyCores := parseCoreRange(numaNode.Cores.Phy)
	htCores := parseCoreRange(numaNode.Cores.HT)

	logger.Info("Available physical cores: %s (%d cores)", numaNode.Cores.Phy, len(phyCores))
	logger.Info("Available HT cores: %s (%d cores)", numaNode.Cores.HT, len(htCores))

	var selectedCores []int

	if phyOnly {
		logger.Info("Using only physical cores as requested")
		selectedCores = phyCores
	} else if htOnly {
		logger.Info("Using only HT cores as requested")
		selectedCores = htCores
	} else {
		logger.Info("Using both physical and HT cores")
		selectedCores = append(selectedCores, phyCores...)
		selectedCores = append(selectedCores, htCores...)
	}

	sort.Ints(selectedCores)

	result := formatCoreRange(selectedCores)
	logger.Info("Selected cores: %s (%d cores)", result, len(selectedCores))

	return result
}
