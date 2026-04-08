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
	"gopkg.in/yaml.v3"
)

// CONFIGURAÇÃO
const MaxConcurrentTools = 20

type Config struct {
	ApiKeys map[string][]string `yaml:"api_keys"`
}

var printMutex sync.Mutex

func loadConfig() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	configDir := filepath.Join(home, ".config", "sfinder")
	filePath := filepath.Join(configDir, "config.yaml")

	os.MkdirAll(configDir, 0755)

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		defaultConfig := Config{
			ApiKeys: map[string][]string{
				"bevigil":             {},
				"binaryedge":          {},
				"builtwith":           {},
				"c99":                 {},
				"censys_api_id":       {},
				"censys_api_secret":   {},
				"certspotter":         {},
				"chaos":               {},
				"facebook_app_id":     {},
				"facebook_app_secret": {},
				"fofa":                {},
				"fullhunt":            {},
				"github":              {},
				"hunter":              {},
				"intelx":              {},
				"leakix":              {},
				"quake":               {},
				"robtex":              {},
				"securitytrails":      {},
				"shodan":              {},
				"urlscan":             {},
				"virustotal":          {},
				"zoomeye":             {},
			},
		}
		data, _ := yaml.Marshal(&defaultConfig)
		os.WriteFile(filePath, data, 0644)
		return &defaultConfig, nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	if config.ApiKeys == nil {
		config.ApiKeys = make(map[string][]string)
	}

	// Recarrega chaves do ambiente (especialmente para ROTAÇÃO)
	var envVTKeys []string
	vtKeysEnv := []string{"VT_API_KEY", "VT_API_KEY2", "VT_API_KEY3", "VT_API_KEY4", "VT_API_KEY5", "VT_API_KEY6", "VT_API_KEY7", "VT_API_KEY8", "VT_API_KEY9", "VT_API_KEY10"}
	for _, envVar := range vtKeysEnv {
		if val := os.Getenv(envVar); val != "" {
			envVTKeys = append(envVTKeys, val)
		}
	}

	if len(envVTKeys) > 0 {
		config.ApiKeys["virustotal"] = envVTKeys
	}

	return &config, nil
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

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

func isValidDomain(sub string) bool {
	for _, r := range sub {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.' || r == '*' {
			continue
		}
		return false
	}
	return true
}

func cleanDomainLine(line, domain string) (cleaned string, isWildcard bool, isValid bool) {
	line = strings.TrimSpace(line)
	line = strings.Trim(line, `"'`)
	line = strings.ToLower(line)
	line = strings.TrimLeft(line, ".")

	if line == "" {
		return "", false, false
	}

	if !isValidDomain(line) {
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

func runShellCommand(command string, errFile string, verbose bool) error {
	if verbose {
		color.New(color.FgHiBlack).Printf("[CMD] %s\n", command)
	}

	cmd := exec.Command("/usr/bin/zsh", "-c", command)

	// Salva Stderr em arquivo para debugging
	file, err := os.Create(errFile)
	if err == nil {
		cmd.Stderr = file
		defer file.Close()
	} else if verbose {
		color.Red("  ✖ Error creating error log file %s: %v\n", errFile, err)
	}

	cmdErr := cmd.Run()

	return cmdErr
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

// runTool com visual moderno, paralelo e REDUNDÂNCIA de chaves
func runTool(commandTemplate, toolName string, keys []string, outputFile string, verbose bool, domain string, wildcardsFile string) {
	start := time.Now()
	prevCount := countLines(outputFile)

	var lastErr error
	var finalFullErr string
	currentCount := countLines(outputFile)
	var countInRun int

	tmpFile := outputFile + ".tmp_run"
	errFile := outputFile + ".err"

	// Garante pelo menos uma iteração para ferramentas sem chave
	loopKeys := keys
	if len(loopKeys) == 0 {
		if toolName == "crtsh" {
			loopKeys = []string{"", "", ""} // 3 tentativas para crt.sh
		} else {
			loopKeys = []string{""}
		}
	}

	for i, k := range loopKeys {
		command := strings.ReplaceAll(commandTemplate, "{KEY}", k)
		fullCommand := fmt.Sprintf("%s > '%s'", command, tmpFile)
		lastErr = runShellCommand(fullCommand, errFile, verbose)

		sortAndDeduplicateFile(tmpFile, domain, wildcardsFile)
		countInRun = countLines(tmpFile)

		if fileExists(tmpFile) && countInRun > 0 {
			appendCmd := fmt.Sprintf("cat '%s' >> '%s'", tmpFile, outputFile)
			runShellCommand(appendCmd, "/dev/null", false)
			os.Remove(tmpFile)
		}

		sortAndDeduplicateFile(outputFile, domain, wildcardsFile)
		currentCount = countLines(outputFile)

		// Diagnóstico para rotação
		fullErrText := ""
		if errContent, rerr := os.ReadFile(errFile); rerr == nil {
			fullErrText += string(errContent) + " "
		}
		if tmpContent, terr := os.ReadFile(tmpFile); terr == nil {
			fullErrText += string(tmpContent) + " "
		}
		rawFile := fmt.Sprintf("/tmp/%s.raw", toolName)
		if rawContent, rerr := os.ReadFile(rawFile); rerr == nil {
			fullErrText += string(rawContent)
			os.Remove(rawFile)
		}
		finalFullErr = fullErrText

		// Se teve sucesso e trouxe algo, para por aqui
		if lastErr == nil && countInRun > 0 {
			break
		}

		// Retry específico para crt.sh (que oscila muito)
		if toolName == "crtsh" && i < len(loopKeys)-1 {
			if verbose {
				fmt.Printf("   [DEBUG] %s: Timeout ou vazio, tentando novamente (%d/%d)...\n", toolName, i+2, len(loopKeys))
			}
			time.Sleep(3 * time.Second)
			continue
		}

		body := strings.ToLower(finalFullErr)
		isLimit := strings.Contains(body, "rate limit") || strings.Contains(body, "too many requests") || strings.Contains(body, "limit exceeded") || strings.Contains(body, "usage limit") || strings.Contains(body, "api count exceeded") || strings.Contains(body, "quota")
		isAuth := strings.Contains(body, "forbidden") || strings.Contains(body, "unauthorized") || strings.Contains(body, "credit") || strings.Contains(body, "balance") || strings.Contains(body, "insufficient") || strings.Contains(body, "invalid token") || strings.Contains(body, "authorization required")

		if (isLimit || isAuth) && i < len(loopKeys)-1 {
			if verbose {
				fmt.Printf("   [DEBUG] %s: Tentando próxima chave de API...\n", toolName)
			}
			continue
		}
		break
	}

	elapsed := time.Since(start).Round(time.Second)
	newInThisTool := currentCount - prevCount

	toolLabel := fmt.Sprintf("%-12s", strings.ToUpper(toolName))
	timeLabel := fmt.Sprintf("%6s", elapsed)
	foundLabel := fmt.Sprintf("%6d found", countInRun)
	totalLabel := fmt.Sprintf("%8d unique", currentCount)

	var newLabel string
	if newInThisTool > 0 {
		newLabel = colorNew(fmt.Sprintf("+%d new", newInThisTool))
	} else {
		newLabel = colorZero("0 new")
	}

	var statusIcon = iconCheck
	var errorLabel string
	if lastErr != nil {
		statusIcon = color.New(color.FgRed, color.Bold).Sprint("✖")
		var cause string
		body := strings.ToLower(finalFullErr)

		if strings.Contains(body, "rate limit") || strings.Contains(body, "too many requests") || strings.Contains(body, "limit exceeded") || strings.Contains(body, "usage limit") || strings.Contains(body, "api count exceeded") || strings.Contains(body, "quota") {
			cause = "- Rate Limited"
		} else if strings.Contains(body, "credit") || strings.Contains(body, "balance") || strings.Contains(body, "insufficient") || strings.Contains(body, "余额不足") {
			cause = "- No Credits"
		} else if strings.Contains(body, "forbidden") || strings.Contains(body, "unauthorized") || strings.Contains(body, "invalid token") || strings.Contains(body, "authorization required") || strings.Contains(body, "not allowed to access endpoint") {
			cause = "- Auth Error"
		} else if strings.Contains(body, "parse error") || strings.Contains(body, "cannot iterate over null") {
			cause = "- Parse Err (Format)"
		} else if strings.Contains(body, "not found") || strings.Contains(body, "check your spelling") || strings.Contains(body, "404") {
			cause = "- No Matches"
		} else if len(strings.TrimSpace(finalFullErr)) > 0 {
			cause = "- Error (See Log)"
		} else {
			cause = "- No Matches"
		}
		errorLabel = color.New(color.FgHiRed).Sprint(fmt.Sprintf(" [%s]", cause))
	}

	printMutex.Lock()
	fmt.Printf(" %s %s  %s  %s  %s  %s%s\n",
		statusIcon,
		colorTool(toolLabel),
		colorTime(timeLabel),
		foundLabel,
		totalLabel,
		newLabel,
		errorLabel,
	)

	if verbose && lastErr != nil {
		color.New(color.FgHiBlack).Printf("\n--- ERROR LOG: %s ---\n", toolName)
		fmt.Printf("%s", finalFullErr)
		color.New(color.FgHiBlack).Printf("-----------------------\n\n")
	}
	printMutex.Unlock()
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
	subdomainToTools := make(map[string][]string)
	allDomains := make(map[string]bool)

	for tool, file := range toolFiles {
		if _, err := os.Stat(file); err == nil {
			data, _ := os.ReadFile(file)
			scanner := bufio.NewScanner(strings.NewReader(string(data)))
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line != "" {
					subdomainToTools[line] = append(subdomainToTools[line], tool)
					allDomains[line] = true
				}
			}
		}
	}

	exclusiveCounts := make(map[string]int)
	for _, tools := range subdomainToTools {
		if len(tools) == 1 {
			exclusiveCounts[tools[0]]++
		}
	}

	cyan := color.New(color.FgCyan).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()

	fmt.Println("\n" + cyan("📊 Exclusive Subdomains Found by ONLY ONE Tool:"))
	type kv struct {
		Key   string
		Value int
	}
	var sortedExclusives []kv
	for k, v := range exclusiveCounts {
		sortedExclusives = append(sortedExclusives, kv{k, v})
	}
	sort.Slice(sortedExclusives, func(i, j int) bool {
		return sortedExclusives[i].Value > sortedExclusives[j].Value
	})

	for _, entry := range sortedExclusives {
		fmt.Printf("   %s: %s exclusive subdomains\n", yellow(strings.ToUpper(entry.Key)), green(fmt.Sprintf("%d", entry.Value)))
	}
	fmt.Printf("\n%sTOTAL UNIQUE Subdomains ACROSS ALL TOOLS: %s\n", cyan(""), green(fmt.Sprintf("%d", len(allDomains))))
}

func discovery(domain string, folderName string, compare bool, toolsArg string, verbose bool) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	baseDir := folderName
	subdomainsDir := filepath.Join(baseDir, "subdomains")
	// Convert Windows paths to Unix-style for shell commands
	subdomainsDir = strings.ReplaceAll(subdomainsDir, "\\", "/")
	os.MkdirAll(subdomainsDir, 0755)

	subsFile := filepath.Join(subdomainsDir, "subdomains.txt")
	subsFile = strings.ReplaceAll(subsFile, "\\", "/")
	oldGlobalCount := countLines(subsFile)

	if domain != "" {
		printHeader(domain, folderName)
	}

	config, err := loadConfig()
	if err != nil {
		config = &Config{ApiKeys: make(map[string][]string)}
		if verbose {
			color.Red("  ✖ Warning loading config.yaml, using defaults: %v\n", err)
		}
	}

	// Chaves serão lidas dinamicamente do config.ApiKeys para rotação

	// ═══════════════════════════════════════════════════════════════
	// FERRAMENTAS DE BUSCA DE SUBDOMÍNIOS - VERSÃO OTIMIZADA
	// ═══════════════════════════════════════════════════════════════
	// MANTIDAS: Ferramentas com funcionamento comprovado
	// REMOVIDAS: Timeouts, APIs offline, erros de parsing, chaves expiradas
	// ADICIONADAS: Novas fontes gratuitas (2025)
	// ═══════════════════════════════════════════════════════════════

	toolCommands := map[string]string{
		// ✅ LOCAL TOOLS (Sem API Key) - Muito rápidas e confiáveis
		"subfinder":    fmt.Sprintf("subfinder -d %s -all -silent | grep -i %s", domain, domain),
		"subdominator": fmt.Sprintf("subdominator -d %s -s | grep -i %s", domain, domain),
		"assetfinder":  fmt.Sprintf("assetfinder %s | grep -i %s", domain, domain),
		"findomain":    fmt.Sprintf("findomain --target %s -q | grep -i %s", domain, domain),

		// ✅ FREE API SOURCES - Sem rate limit rigoroso
		"alienvault":     fmt.Sprintf("curl -s --compressed 'https://otx.alienvault.com/api/v1/indicators/domain/%s/url_list?limit=100' | jq -r '.url_list[].hostname' | sort -u | grep -i %s", domain, domain),
		"rapiddns":       fmt.Sprintf("curl -s --compressed 'https://rapiddns.io/subdomain/%s?full=1' | grep -oE '[a-zA-Z0-9.-]+\\.%s' | tr '[:upper:]' '[:lower:]' | sort -u", domain, domain),
		"waybackmachine": fmt.Sprintf("curl -s --compressed -m 60 'http://web.archive.org/cdx/search/cdx?url=*.%s/*&output=text&fl=original&collapse=urlkey' | grep -oE '[a-zA-Z0-9.-]+\\.%s' | sort -u", domain, domain),

		// ✅ NEW FREE APIs (2025) - Funcionando bem
		"crtsh":   fmt.Sprintf("curl -s 'https://crt.sh/?q=%%25.%s&output=json' 2>/dev/null | jq -r '.[] | .name_value' 2>/dev/null | grep '%s' | tr '[:upper:]' '[:lower:]' | sort -u", domain, domain),
		"urlscan": fmt.Sprintf("curl -s 'https://urlscan.io/api/v1/search/?q=domain:%s' 2>/dev/null | jq -r '.results[] | .page.domain' 2>/dev/null | tr '[:upper:]' '[:lower:]' | sort -u", domain),

		// ⭐ PAID APIs (Com API Key) - Funcionam bem
		"c99":            fmt.Sprintf("curl -s --compressed 'https://api.c99.nl/subdomainfinder?key={KEY}&domain=%s' 2>/dev/null | jq -r '.subdomains[].subdomain // empty' | grep '%s' | sort -u", domain, domain),
		"certspotter":    fmt.Sprintf("curl -s --compressed -H 'Authorization: Bearer {KEY}' 'https://api.certspotter.com/v1/issuances?domain=%s&include_subdomains=true&expand=dns_names' | jq -r '.[].dns_names[]' 2>/dev/null | sort -u | grep -i %s", domain, domain),
		"chaos":          fmt.Sprintf("chaos -d %s -silent -key '{KEY}' 2>/dev/null | grep -i %s", domain, domain),
		"leakix":         fmt.Sprintf("curl -s --compressed -H 'api-key: {KEY}' 'https://leakix.net/api/subdomains/%s' | jq -r '.[].subdomain?' 2>/dev/null | tr '[:upper:]' '[:lower:]' | sort -u | grep -i %s", domain, domain),
		"pulsedive":      fmt.Sprintf("curl -s --compressed 'https://pulsedive.com/api/info.php?indicator=%s&key={KEY}' 2>/dev/null | jq -r '.properties.dns[]?' | grep -i %s", domain, domain),
		"virustotal":     fmt.Sprintf("url=\"https://www.virustotal.com/api/v3/domains/%s/subdomains?limit=40\"; while [ -n \"$url\" ]; do response=$(curl -s --compressed \"$url\" -H \"x-apikey: {KEY}\" 2>/dev/null); printf '%%s\\n' \"$response\" | jq -r '.data[].id'; url=$(printf '%%s\\n' \"$response\" | jq -r '.links.next // empty'); done | grep -i %s", domain, domain),
		"securitytrails": fmt.Sprintf("curl -s --compressed -H 'apikey: {KEY}' 'https://api.securitytrails.com/v1/domain/%s/subdomains' 2>/dev/null | jq -r '.subdomains[]?' | sed 's/$/.%s/' | grep -i %s", domain, domain, domain),
		"shodan":         fmt.Sprintf("curl -s --compressed 'https://www.shodan.io/domain/%s' 2>/dev/null | egrep -i '<li>.+</li>' | awk -F '<li>' '{print $2}' | awk -F '</li>' '{print $1}' | sed 's/$/.%s/' | grep -i %s", domain, domain, domain),
		"hackertarget":   fmt.Sprintf("curl -s --compressed 'https://api.hackertarget.com/hostsearch/?q=%s' 2>/dev/null | awk -F ',' '{print $1}' | grep -i %s", domain, domain),

		// � PREMIUM RECOMENDADAS (Para adicionar quando tiver chaves):
		// "whoisxml": Base WHOIS + DNS histories - https://www.whoisxmlapi.com/ (~50$/mês)
		// "recon": Consolidação avançada - https://recon.dev/ (~50$/mês)
		// "shodan_api": API oficial Shodan (muito melhor que scraping) - https://developer.shodan.io/ (~49$/mês)
		// "akira": Consolidação de subdomínios (FREE) - https://www.akira.sh/
		// "google_transparency": Google Certificate Transparency Logs (FREE API)

		// �🔴 REMOVED TOOLS - Problemas conhecidos:
		// "shodan" → Scraping HTML (frágil), considerar API oficial paga
		// "anubis" → API offline/timeout
		// "binaryedge" → JSON parsing error (chave inválida: "binaryedge")
		// "crtsh" → Substituído por crtshapi (mais rápido)
		// "criminalip" → Consistente timeout
		// "facebook" → Credenciais expiradas
		// "fofa" → Timeout/erro rate limit
		// "fullcontact" → Timeout
		// "gau" → Ferramenta não instalada (dependência local)
		// "hackertarget" → Output vazio
		// "hunterhow" → Timeout
		// "intelx" → Timeout
		// "netlas" → Timeout
		// "passivetotal" → Timeout
		// "publicwww" → Timeout
		// "quake" → Timeout
		// "zoomeye" → Timeout
		// "builtwith" → Timeout
		// "threatminer" → Sem output
		// "urlscan" → Sem output
	}

	toolFiles := make(map[string]string)
	for tool := range toolCommands {
		toolFiles[tool] = filepath.Join(subdomainsDir, fmt.Sprintf("%s.txt", tool))
		// Convert Windows paths to Unix-style for shell commands
		toolFiles[tool] = strings.ReplaceAll(toolFiles[tool], "\\", "/")
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
	// Convert Windows paths to Unix-style for shell commands
	wildcardsFile = strings.ReplaceAll(wildcardsFile, "\\", "/")

	for _, tool := range selectedTools {
		if cmdStr, exists := toolCommands[tool]; exists {
			wg.Add(1)
			go func(t, c string) {
				defer wg.Done()
				sem <- struct{}{}
				keysList := config.ApiKeys[t]
				runTool(c, t, keysList, toolFiles[t], verbose, domain, wildcardsFile)
				<-sem
			}(tool, cmdStr)
		} else {
			color.Red(fmt.Sprintf("  ✖ Invalid tool: %s\n", tool))
		}
	}
	wg.Wait()

	aggregateAndClean(toolFiles, subsFile, oldGlobalCount, domain, wildcardsFile)

	filterUniquePerTool(toolFiles)

	compareUniqueDomains(toolFiles)
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
