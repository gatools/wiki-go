package goldext

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"wiki-go/internal/config"
)

// Store extracted PlantUML blocks until after Goldmark processing
var (
	plantumlBlocks     = make(map[string]string)
	plantumlBlockCount = 0
	plantumlMutex      sync.Mutex
)

// PlantUMLPreprocessor extracts plantuml blocks and replaces them with placeholders
// that Goldmark won't process. The blocks will be restored after Goldmark rendering.
func PlantUMLPreprocessor(markdown string, _ string) string {
	plantumlMutex.Lock()
	defer plantumlMutex.Unlock()

	// Reset the storage on each new document
	plantumlBlocks = make(map[string]string)
	plantumlBlockCount = 0

	// Process line by line to safely extract plantuml blocks
	lines := strings.Split(markdown, "\n")
	var result []string

	inPlantUMLBacktick := false
	inPlantUMLTilde := false
	plantumlContent := []string{}

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Detect start/end of plantuml blocks
		if trimmed == "```plantuml" {
			inPlantUMLBacktick = true
			plantumlContent = []string{}
			continue
		} else if trimmed == "```" && inPlantUMLBacktick {
			inPlantUMLBacktick = false
			// Generate a placeholder that Goldmark won't touch
			blockID := fmt.Sprintf("MERMAID_BLOCK_%d", plantumlBlockCount)
			plantumlBlockCount++
			// Store the actual plantuml div
			plantumlDiv := "<div class=\"plantuml\">" + GetRemoteDiagram(strings.Join(plantumlContent, "\n"), config.Cfg, false) + "</div>"
			plantumlBlocks[blockID] = plantumlDiv
			// Add placeholder to output - this will pass through Goldmark untouched
			result = append(result, "<!-- "+blockID+" -->")
			continue
		} else if trimmed == "~~~plantuml" {
			inPlantUMLTilde = true
			plantumlContent = []string{}
			continue
		} else if trimmed == "~~~" && inPlantUMLTilde {
			inPlantUMLTilde = false
			// Generate a placeholder that Goldmark won't touch
			blockID := fmt.Sprintf("MERMAID_BLOCK_%d", plantumlBlockCount)
			plantumlBlockCount++
			// Store the actual plantuml div
			plantumlDiv := "<div class=\"plantuml\">" + GetRemoteDiagram(strings.Join(plantumlContent, "\n"), config.Cfg, false) + "</div>"
			plantumlBlocks[blockID] = plantumlDiv
			// Add placeholder to output - this will pass through Goldmark untouched
			result = append(result, "<!-- "+blockID+" -->")
			continue
		}

		// Collect content or pass unchanged
		if inPlantUMLBacktick || inPlantUMLTilde {
			plantumlContent = append(plantumlContent, line)
		} else {
			result = append(result, line)
		}
	}

	// Handle any unclosed blocks (rare, but possible)
	if inPlantUMLBacktick || inPlantUMLTilde {
		blockID := fmt.Sprintf("MERMAID_BLOCK_%d", plantumlBlockCount)
		plantumlBlockCount++
		plantumlDiv := "<div class=\"plantuml\">" + GetRemoteDiagram(strings.Join(plantumlContent, "\n"), config.Cfg, false) + "</div>"
		plantumlBlocks[blockID] = plantumlDiv
		result = append(result, "<!-- "+blockID+" -->")
	}

	return strings.Join(result, "\n")
}

func GetRemoteDiagram(code string, cfg *config.Config, dark bool) string {
	// If PlantUML is not enabled or server URL is not set, return the code as-is
	if !cfg.Extensions.PlantUML.Enable || cfg.Extensions.PlantUML.ServerURL == "" {
		return fmt.Sprintf("<p>%v</p>", code)
	}

	// Encode the PlantUML code to a URL-safe format
	encodedCode := EncodeCode(code)

	// Determine the prefix based on dark mode
	var darkPrefix string
	if dark {
		darkPrefix = "d"
	} else {
		darkPrefix = ""
	}

	// Construct the full URL for the PlantUML server
	url := fmt.Sprintf(
		"%s/%s%s/%s",
		cfg.Extensions.PlantUML.ServerURL,
		darkPrefix,
		cfg.Extensions.PlantUML.ImageFormat,
		encodedCode,
	)

	// Do request to fetch the content
	contentRequest, err := http.Get(url)
	if err != nil {
		return fmt.Sprintf("<p>Error fetching PlantUML diagram: %v</p>", err)
	}
	defer contentRequest.Body.Close()

	content, err := io.ReadAll(contentRequest.Body)
	if err != nil {
		return fmt.Sprintf("<p>Error reading PlantUML diagram: %v</p>", err)
	}

	return string(content)
}

func Replace(data string) string {
	const plantumlAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-_"
	const base64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

	var sb strings.Builder

	for _, c := range data {
		index := strings.IndexRune(base64Alphabet, c)
		if index != -1 {
			sb.WriteRune(rune(plantumlAlphabet[index]))
		}
		// else {  // Check if needed
		// 	sb.WriteRune(c)
		// }
	}

	return sb.String()
}

func EncodeCode(data string) string {
	var buf bytes.Buffer

	zw := zlib.NewWriter(&buf)
	zw.Write([]byte(data))
	zw.Close()

	compressed := buf.Bytes()
	compressed = compressed[2 : len(compressed)-4]

	encodedStr := base64.StdEncoding.EncodeToString(compressed)
	return Replace(encodedStr)
}

// RestorePlantUMLBlocks replaces placeholders with actual plantuml diagrams
// This must be called after Goldmark processing
func RestorePlantUMLBlocks(html string) string {
	plantumlMutex.Lock()
	defer plantumlMutex.Unlock()

	result := html
	for id, block := range plantumlBlocks {
		placeholder := fmt.Sprintf("<!-- %s -->", id)
		result = strings.Replace(result, placeholder, block, 1)
	}

	return result
}
