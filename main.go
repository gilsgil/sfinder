package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/briandowns/spinner"
	"github.com/common-nighthawk/go-figure"
	"github.com/fatih/color"
)

// CONFIGURAÇÃO
const MaxConcurrentTools = 1

// --- HELPERS VISUAIS ---

// Ícones e cores modernas
var (
	iconCheck = color.New(color.FgGreen, color.Bold).Sprint("✔")
	iconFire  = color.New(color.FgHiYellow).Sprint("⚡")
	iconBox   = color.New(color.FgCyan).Sprint("📦")

	colorTool = color.New(color.FgHiWhite, color.Bold).SprintFunc()
	colorTime = color.New(color.FgHiBlack).SprintFunc() // Cinza escuro para o tempo
	colorNew  = color.New(color.FgHiGreen, color.Bold).SprintFunc()
	colorZero = color.New(color.FgHiBlack).SprintFunc() // Discreto se for zero
)

func printBanner() {
	// Limpa a tela antes de começar
	fmt.Print("\033[H\033[2J")

	myFigure := figure.NewFigure("SFINDER", "slant", true)
	color.Cyan(myFigure.String())
	fmt.Println(color.New(color.FgHiBlack).Sprint("\n   by Gilson Oliveira"))
	fmt.Println("")
}

func printHeader(domain, folder string) {
	fmt.Printf("   %s Target: %s\n", iconFire, color.HiWhiteString(domain))
	fmt.Printf("   %s Output: %s\n", iconBox, color.HiWhiteString(folder))
	fmt.Println(strings.Repeat(color.HiBlackString("─"), 60))
	fmt.Println("")
}

// --- HELPERS LÓGICOS ---

var wildcardMutex sync.Mutex

func cleanDomainLine(line, domain string) (cleaned string, isWildcard bool, isValid bool) {
	line = strings.TrimSpace(line)
	line = strings.Trim(line, `"'`)
	line = strings.ToLower(line)
	line = strings.TrimLeft(line, ".")

	if line == "" {
		return "", false, false
	}

	if !strings.HasSuffix(line, domain) {
		return "", false, false
	}
	if len(line) > len(domain) && !strings.HasSuffix(line, "."+domain) {
		return "", false, false
	}

	if strings.Contains(line, "*") {
		// Normaliza wildcards
		for strings.Contains(line, "**") {
			line = strings.ReplaceAll(line, "**", "*")
		}
		if strings.HasPrefix(line, "*") && !strings.HasPrefix(line, "*.") {
			if line == "*"+domain {
				line = "*." + domain
			} else {
				line = strings.Replace(line, "*", "*.", 1)
			}
		}
		// Corrige possíveis pontos duplos
		line = strings.ReplaceAll(line, "..", ".")
		return line, true, true
	}

	return line, false, true
}

func fileExists(filePath string) bool {
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func countLines(filePath string) int {
	if !fileExists(filePath) {
		return 0
	}
	file, err := os.Open(filePath)
	if err != nil {
		return 0
	}
	defer file.Close()
	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			count++
		}
	}
	return count
}

func runShellCommand(command string, verbose bool) error {
	if verbose {
		color.New(color.FgHiBlack).Printf("[CMD] %s\n", command)
	}
	cmd := exec.Command("zsh", "-i", "-c", command)
	if verbose {
		cmd.Stderr = os.Stderr
	}
	return cmd.Run()
}

func sortAndDeduplicateFile(filePath string, domain string, wildcardsFile string) error {
	if !fileExists(filePath) {
		return nil
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	lines := strings.Split(string(content), "\n")
	uniqueMap := make(map[string]bool)
	wildcardsMap := make(map[string]bool)

	for _, line := range lines {
		cleaned, isWildcard, isValid := cleanDomainLine(line, domain)
		if !isValid {
			continue
		}
		if isWildcard {
			wildcardsMap[cleaned] = true
		} else {
			uniqueMap[cleaned] = true
		}
	}

	var uniqueLines []string
	for line := range uniqueMap {
		uniqueLines = append(uniqueLines, line)
	}
	sort.Strings(uniqueLines)
	if err := os.WriteFile(filePath, []byte(strings.Join(uniqueLines, "\n")+"\n"), 0644); err != nil {
		return err
	}

	if len(wildcardsMap) > 0 {
		wildcardMutex.Lock()
		defer wildcardMutex.Unlock()

		existingWildcards := make(map[string]bool)
		if fileExists(wildcardsFile) {
			if wc, err := os.ReadFile(wildcardsFile); err == nil {
				for _, wl := range strings.Split(string(wc), "\n") {
					wl = strings.TrimSpace(wl)
					if wl != "" {
						existingWildcards[wl] = true
					}
				}
			}
		}
		for w := range wildcardsMap {
			existingWildcards[w] = true
		}
		var wLines []string
		for w := range existingWildcards {
			wLines = append(wLines, w)
		}
		sort.Strings(wLines)
		os.WriteFile(wildcardsFile, []byte(strings.Join(wLines, "\n")+"\n"), 0644)
	}

	return nil
}

// runTool com visual moderno e spinner
func runTool(command, toolName, outputFile string, verbose bool, domain string, wildcardsFile string) {
	start := time.Now()
	prevCount := countLines(outputFile)

	// Inicia Spinner
	s := spinner.New(spinner.CharSets[14], 100*time.Millisecond) // Estilo "dots"
	s.Suffix = fmt.Sprintf("  Running %s...", colorTool(strings.ToUpper(toolName)))
	s.Color("cyan")
	s.Start()

	// --- Lógica de Execução ---

	fullCommand := fmt.Sprintf("%s >> %s", command, outputFile)
	runShellCommand(fullCommand, verbose)

	// Ordenação individual nativa
	sortAndDeduplicateFile(outputFile, domain, wildcardsFile)
	// ---------------------------------------------

	s.Stop() // Para o spinner

	// Estatísticas
	elapsed := time.Since(start).Round(time.Second)
	currentCount := countLines(outputFile)
	newInThisTool := currentCount - prevCount

	// Formatação Visual (Alinhamento em colunas)
	toolLabel := fmt.Sprintf("%-12s", strings.ToUpper(toolName))
	timeLabel := fmt.Sprintf("%6s", elapsed)
	totalLabel := fmt.Sprintf("%8d subs", currentCount)

	var newLabel string
	if newInThisTool > 0 {
		newLabel = colorNew(fmt.Sprintf("+%d new", newInThisTool))
	} else {
		newLabel = colorZero("0 new")
	}

	// Output final da linha
	fmt.Printf(" %s %s  %s  %s  %s\n",
		iconCheck,
		colorTool(toolLabel),
		colorTime(timeLabel),
		totalLabel,
		newLabel,
	)
}

func aggregateAndClean(toolFiles map[string]string, subsFile string, oldGlobalCount int, domain string, wildcardsFile string) {
	// Spinner para a agregação
	fmt.Println("")
	s := spinner.New(spinner.CharSets[11], 100*time.Millisecond)
	s.Suffix = "  Aggregating and deduplicating results..."
	s.Color("yellow")
	s.Start()

	rawCombined := subsFile + ".tmp"
	os.Remove(rawCombined)

	// Salvar subs antigos antes de agregar (para calcular diff depois)
	oldSubs := make(map[string]bool)
	if fileExists(subsFile) {
		oldContent, err := os.ReadFile(subsFile)
		if err == nil {
			for _, line := range strings.Split(string(oldContent), "\n") {
				cleaned, isWildcard, isValid := cleanDomainLine(line, domain)
				if isValid && !isWildcard {
					oldSubs[cleaned] = true
				}
			}
		}
	}

	var filesToMerge []string
	if fileExists(subsFile) {
		filesToMerge = append(filesToMerge, subsFile)
	}
	for _, f := range toolFiles {
		if fileExists(f) {
			filesToMerge = append(filesToMerge, f)
		}
	}

	if len(filesToMerge) > 0 {
		var allLines []string
		for _, f := range filesToMerge {
			content, err := os.ReadFile(f)
			if err == nil {
				lines := strings.Split(string(content), "\n")
				for _, line := range lines {
					if strings.TrimSpace(line) != "" {
						allLines = append(allLines, strings.TrimSpace(line))
					}
				}
			}
		}

		uniqueMap := make(map[string]bool)
		wildcardsMap := make(map[string]bool)
		for _, line := range allLines {
			cleaned, isWildcard, isValid := cleanDomainLine(line, domain)
			if !isValid {
				continue
			}
			if isWildcard {
				wildcardsMap[cleaned] = true
			} else {
				uniqueMap[cleaned] = true
			}
		}

		var uniqueLines []string
		for line := range uniqueMap {
			uniqueLines = append(uniqueLines, line)
		}
		sort.Strings(uniqueLines)
		os.WriteFile(subsFile, []byte(strings.Join(uniqueLines, "\n")+"\n"), 0644)

		if len(wildcardsMap) > 0 {
			existingWildcards := make(map[string]bool)
			if fileExists(wildcardsFile) {
				if wc, err := os.ReadFile(wildcardsFile); err == nil {
					for _, wl := range strings.Split(string(wc), "\n") {
						wl = strings.TrimSpace(wl)
						if wl != "" {
							existingWildcards[wl] = true
						}
					}
				}
			}
			for w := range wildcardsMap {
				existingWildcards[w] = true
			}
			var wLines []string
			for w := range existingWildcards {
				wLines = append(wLines, w)
			}
			sort.Strings(wLines)
			os.WriteFile(wildcardsFile, []byte(strings.Join(wLines, "\n")+"\n"), 0644)
		}
	}

	s.Stop()

	// Calcular subs novos (diff entre arquivo final e antigas)
	var newSubs []string
	if fileExists(subsFile) {
		newContent, err := os.ReadFile(subsFile)
		if err == nil {
			for _, line := range strings.Split(string(newContent), "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !oldSubs[line] {
					newSubs = append(newSubs, line)
				}
			}
		}
	}

	// Ordenar os novos subs (ascending)
	sort.Strings(newSubs)

	// Salvar last_results.txt
	lastResultsFile := filepath.Join(filepath.Dir(subsFile), "last_results.txt")
	if len(newSubs) > 0 {
		os.WriteFile(lastResultsFile, []byte(strings.Join(newSubs, "\n")+"\n"), 0644)
	} else {
		// Arquivo vazio se não houver novos subs
		os.WriteFile(lastResultsFile, []byte{}, 0644)
	}

	// Stats Finais
	newGlobalCount := countLines(subsFile)
	realNewSubs := len(newSubs)

	// Caixa de Resumo Moderno
	fmt.Println("")
	fmt.Println(color.HiBlackString("┌──────────────────────────────────────────────┐"))
	fmt.Printf("│  %s                 │\n", color.HiWhiteString("FINAL RESULTS SUMMARY"))
	fmt.Println(color.HiBlackString("├──────────────────────────────────────────────┤"))
	fmt.Printf("│  Previous Total     : %-22d │\n", oldGlobalCount)
	fmt.Printf("│  Current Total      : %-22d │\n", newGlobalCount)
	fmt.Println(color.HiBlackString("│                                              │"))

	if realNewSubs > 0 {
		fmt.Printf("│  %s : %-22s │\n", color.HiGreenString("UNIQUE NEW SUBS"), colorNew(fmt.Sprintf("+%d", realNewSubs)))
	} else {
		fmt.Printf("│  %s           : %-22s │\n", "Unique New Subs", color.HiBlackString("0"))
	}
	fmt.Println(color.HiBlackString("└──────────────────────────────────────────────┘"))

	// Mostrar os novos subs no terminal (ordenados ascending)
	if len(newSubs) > 0 {
		fmt.Println("")
		fmt.Println(color.HiCyanString("┌──────────────────────────────────────────────┐"))
		fmt.Printf("│  %s                       │\n", color.HiWhiteString("NEW SUBS FOUND"))
		fmt.Println(color.HiCyanString("└──────────────────────────────────────────────┘"))
		for _, sub := range newSubs {
			fmt.Println(sub)
		}
	}
	fmt.Println("")
}

func filterUniquePerTool(toolFiles map[string]string) {
	cyan := color.New(color.FgCyan).SprintFunc()
	fmt.Printf("\n%s Filtering original tool outputs to keep only truly exclusive entries...\n", cyan(""))

	frequency := make(map[string]int)
	for _, file := range toolFiles {
		if _, err := os.Stat(file); err == nil {
			data, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			scanner := bufio.NewScanner(strings.NewReader(string(data)))
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line != "" {
					frequency[line]++
				}
			}
		}
	}

	for _, file := range toolFiles {
		if _, err := os.Stat(file); err == nil {
			data, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			scanner := bufio.NewScanner(strings.NewReader(string(data)))
			filtered := []string{}
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line != "" && frequency[line] == 1 {
					filtered = append(filtered, line)
				}
			}
			sort.Strings(filtered)
			tmpFile := file + ".tmp"
			if err := os.WriteFile(tmpFile, []byte(strings.Join(filtered, "\n")+"\n"), 0644); err == nil {
				os.Rename(tmpFile, file)
			}
		}
	}
}

func compareUniqueDomains(toolFiles map[string]string) {
	allDomains := make(map[string]bool)
	toolUniqueCounts := make(map[string]int)

	for tool, file := range toolFiles {
		if _, err := os.Stat(file); err == nil {
			data, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			scanner := bufio.NewScanner(strings.NewReader(string(data)))
			domains := make(map[string]bool)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line != "" {
					domains[line] = true
				}
			}
			uniqueCount := 0
			for domain := range domains {
				if !allDomains[domain] {
					uniqueCount++
					allDomains[domain] = true
				}
			}
			toolUniqueCounts[tool] = uniqueCount
		}
	}

	cyan := color.New(color.FgCyan).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	magenta := color.New(color.FgMagenta).SprintFunc()
	fmt.Println("\n" + cyan("Comparison of Unique Subdomains Per Tool:"))

	type kv struct {
		Key   string
		Value int
	}
	var sortedTools []kv
	for k, v := range toolUniqueCounts {
		sortedTools = append(sortedTools, kv{k, v})
	}
	sort.Slice(sortedTools, func(i, j int) bool {
		return sortedTools[i].Value > sortedTools[j].Value
	})
	for _, entry := range sortedTools {
		fmt.Printf("%s: %s unique subdomains\n", yellow(strings.ToUpper(entry.Key)), green(fmt.Sprintf("%d", entry.Value)))
	}
	fmt.Printf("\n%sTOTAL UNIQUE Subdomains ACROSS ALL TOOLS: %s\n", magenta(""), green(fmt.Sprintf("%d", len(allDomains))))
}

func discovery(domain string, folderName string, compare bool, toolsArg string, verbose bool) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	baseDir := folderName
	subdomainsDir := filepath.Join(baseDir, "subdomains")
	os.MkdirAll(subdomainsDir, 0755)

	subsFile := filepath.Join(subdomainsDir, "subdomains.txt")
	oldGlobalCount := countLines(subsFile)

	if domain != "" {
		printHeader(domain, folderName)
	}

	chaosKey := os.Getenv("CHAOS")
	vtAPIKey := os.Getenv("VT_API_KEY")

	toolCommands := map[string]string{
		"subfinder":    fmt.Sprintf("subfinder -d %s -all -silent | grep -i %s", domain, domain),
		"subdominator": fmt.Sprintf("subdominator -d %s -s | grep -i %s", domain, domain),
		"assetfinder":  fmt.Sprintf("assetfinder %s | grep -i %s", domain, domain),
		"findomain":    fmt.Sprintf("findomain --target %s -q | grep -i %s", domain, domain),
		"chaos":        fmt.Sprintf("chaos -d %s -silent -key %s | grep -i %s", domain, chaosKey, domain),
		"virustotal": fmt.Sprintf("url=\"https://www.virustotal.com/api/v3/domains/%s/subdomains?limit=40\"; "+
			"while [ -n \"$url\" ]; do response=$(curl -s \"$url\" -H \"x-apikey: %s\"); "+
			"echo \"$response\" | jq -r \".data[].id\"; "+
			"url=$(echo \"$response\" | jq -r \".links.next // empty\"); done | grep -i %s", domain, vtAPIKey, domain),
		"shrewdeye":   fmt.Sprintf("curl -s 'https://shrewdeye.app/domains/%s.txt' | grep -i %s | egrep -v '<|>'", domain, domain),
		"shodan":      fmt.Sprintf("curl -s 'https://www.shodan.io/domain/%s' | egrep -i '<li>.+</li>' | awk -F '<li>' '{print $2}' | awk -F '</li>' '{print $1}' | sed 's/$/.%s/' | grep -i %s", domain, domain, domain),
		"crtsh":       fmt.Sprintf("curl -s 'https://crt.sh/?q=%%25.%s&output=json' | jq -r 'map(select(.name_value != null)) | .[].name_value' | sed 's/\\*\\.//g' | tr '[:upper:]' '[:lower:]' | sort -u | grep -i %s", domain, domain),
		"certspotter": fmt.Sprintf("curl -s 'https://api.certspotter.com/v1/issuances?domain=%s&include_subdomains=true&expand=dns_names' | jq -r '.[].dns_names[]' | sort -u | grep -i %s", domain, domain),
	}

	toolFiles := make(map[string]string)
	for tool := range toolCommands {
		toolFiles[tool] = filepath.Join(subdomainsDir, fmt.Sprintf("%s.txt", tool))
	}

	if domain == "" {
		if compare {
			compareUniqueDomains(toolFiles)
		} else {
			color.Red("Error: You must provide a domain (-d) or use -c to compare existing results.")
		}
		return
	}

	var selectedTools []string
	if toolsArg == "" {
		for tool := range toolCommands {
			selectedTools = append(selectedTools, tool)
		}
	} else {
		parts := strings.Split(toolsArg, ",")
		for _, part := range parts {
			selectedTools = append(selectedTools, strings.TrimSpace(part))
		}
	}

	sem := make(chan struct{}, MaxConcurrentTools)
	var wg sync.WaitGroup
	wildcardsFile := filepath.Join(subdomainsDir, "wildcards.txt")

	for _, tool := range selectedTools {
		if cmdStr, exists := toolCommands[tool]; exists {
			wg.Add(1)
			go func(t, c string) {
				defer wg.Done()
				sem <- struct{}{}
				runTool(c, t, toolFiles[t], verbose, domain, wildcardsFile)
				<-sem
			}(tool, cmdStr)
		} else {
			color.Red(fmt.Sprintf("  ✖ Invalid tool: %s\n", tool))
		}
	}
	wg.Wait()

	aggregateAndClean(toolFiles, subsFile, oldGlobalCount, domain, wildcardsFile)

	filterUniquePerTool(toolFiles)

	if compare {
		compareUniqueDomains(toolFiles)
	}
}

func init() {
	// Garante que ~/go/bin esteja no PATH para ferramentas instaladas via go install
	home, err := os.UserHomeDir()
	if err == nil {
		goBin := filepath.Join(home, "go", "bin")
		path := os.Getenv("PATH")
		if !strings.Contains(path, goBin) {
			os.Setenv("PATH", goBin+string(os.PathListSeparator)+path)
		}
	}
}

func main() {
	domain := flag.String("d", "", "Target domain")
	folderName := flag.String("f", "", "Output folder name")
	compare := flag.Bool("c", false, "Compare unique subdomains found per tool")
	tools := flag.String("t", "", "Run specific tool(s), comma-separated (e.g., subfinder,assetfinder)")
	verbose := flag.Bool("v", false, "Verbose mode")
	flag.Parse()

	if *folderName == "" {
		fmt.Println("")
		color.Red("  ✖ Error: Missing arguments.")
		fmt.Println("  Usage: sfinder -d domain.com -f output_folder")
		fmt.Println("")
		os.Exit(1)
	}

	printBanner()
	discovery(*domain, *folderName, *compare, *tools, *verbose)
}
