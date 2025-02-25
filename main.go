package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/common-nighthawk/go-figure"
	"github.com/fatih/color"
)

// printBanner exibe o banner ASCII com o texto "SFINDER" no estilo slant.
func printBanner() {
	fig := figure.NewFigure("SFINDER", "slant", true)
	cyan := color.New(color.FgCyan).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	fmt.Println(cyan(fig.String()))
	fmt.Println(yellow("by Gilson Oliveira"))
}

// printStat exibe as estatísticas de cada ferramenta com cores.
func printStat(tool string, count int, newCount int) {
	green := color.New(color.FgGreen, color.Bold).SprintFunc()
	fmt.Printf("%s Subdomains found: %d (New: %d)\n", green("["+strings.ToUpper(tool)+"]"), count, newCount)
}

// runShellCommand executa um comando no shell (usando bash -c).
func runShellCommand(cmd string) error {
	command := exec.Command("bash", "-c", cmd)
	// Silencia stdout e stderr
	command.Stdout = nil
	command.Stderr = nil
	return command.Run()
}

// sortUniqueFile lê o arquivo, remove duplicatas, ordena e regrava.
func sortUniqueFile(filePath string) error {
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	uniqueMap := make(map[string]bool)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			uniqueMap[line] = true
		}
	}
	uniqueLines := []string{}
	for line := range uniqueMap {
		uniqueLines = append(uniqueLines, line)
	}
	sort.Strings(uniqueLines)
	content := strings.Join(uniqueLines, "\n") + "\n"
	return ioutil.WriteFile(filePath, []byte(content), 0644)
}

// runTool executa o comando da ferramenta, grava os resultados e realiza o controle de entradas novas.
func runTool(command string, toolName string, outputFile string) (int, int, error) {
	rawFile := outputFile + ".raw"
	oldSet := make(map[string]bool)

	// Carrega os subdomínios antigos do arquivo raw (se existir)
	if _, err := os.Stat(rawFile); err == nil {
		data, err := ioutil.ReadFile(rawFile)
		if err == nil {
			scanner := bufio.NewScanner(strings.NewReader(string(data)))
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line != "" {
					oldSet[line] = true
				}
			}
		}
	}
	// Cria o diretório de saída, se necessário
	outDir := filepath.Dir(outputFile)
	os.MkdirAll(outDir, os.ModePerm)

	// Executa o comando com pipe para "anew" e grava em outputFile
	fullCmd := fmt.Sprintf("%s | anew %s", command, outputFile)
	if err := runShellCommand(fullCmd); err != nil {
		return 0, 0, fmt.Errorf("%s error: %v", toolName, err)
	}

	// Lê os resultados atuais do arquivo de saída
	newSet := make(map[string]bool)
	if _, err := os.Stat(outputFile); err == nil {
		data, err := ioutil.ReadFile(outputFile)
		if err == nil {
			scanner := bufio.NewScanner(strings.NewReader(string(data)))
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line != "" {
					newSet[line] = true
				}
			}
		}
	}
	newCount := len(newSet)

	// Calcula os itens novos (delta)
	delta := []string{}
	for domain := range newSet {
		if !oldSet[domain] {
			delta = append(delta, domain)
		}
	}
	sort.Strings(delta)

	// Grava os itens novos em last_{tool}.txt
	lastFile := filepath.Join(outDir, fmt.Sprintf("last_%s.txt", toolName))
	err := ioutil.WriteFile(lastFile, []byte(strings.Join(delta, "\n")+"\n"), 0644)
	if err != nil {
		fmt.Printf("Error writing last file for %s: %v\n", toolName, err)
	}
	additional := len(delta)

	// Atualiza o arquivo raw com o conjunto completo atual (ordenado)
	allNew := []string{}
	for domain := range newSet {
		allNew = append(allNew, domain)
	}
	sort.Strings(allNew)
	if err := ioutil.WriteFile(rawFile, []byte(strings.Join(allNew, "\n")+"\n"), 0644); err != nil {
		fmt.Printf("Error writing raw file for %s: %v\n", toolName, err)
	}

	// Remove duplicatas no arquivo de saída (similar a "sort -u -o")
	if err := sortUniqueFile(outputFile); err != nil {
		fmt.Printf("Error sorting file for %s: %v\n", toolName, err)
	}

	return newCount, additional, nil
}

// filterUniquePerTool filtra cada arquivo de ferramenta para manter somente entradas exclusivas.
func filterUniquePerTool(toolFiles map[string]string) {
	// Agrega o conteúdo de todos os arquivos
	frequency := make(map[string]int)
	for _, file := range toolFiles {
		if _, err := os.Stat(file); err == nil {
			data, err := ioutil.ReadFile(file)
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

	// Para cada arquivo, grava apenas as linhas cuja frequência seja 1
	for tool, file := range toolFiles {
		if _, err := os.Stat(file); err == nil {
			data, err := ioutil.ReadFile(file)
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
			if err := ioutil.WriteFile(tmpFile, []byte(strings.Join(filtered, "\n")+"\n"), 0644); err != nil {
				fmt.Printf("Error writing temp file for %s: %v\n", tool, err)
			}
			os.Rename(tmpFile, file)
			// Exibe estatística do arquivo filtrado
			cyan := color.New(color.FgCyan).SprintFunc()
			green := color.New(color.FgGreen).SprintFunc()
			fmt.Printf("%s original file filtered: kept %s truly unique entries.\n", cyan("["+strings.ToUpper(tool)+"]"), green(fmt.Sprintf("%d", len(filtered))))
		}
	}
}

// compareUniqueDomains compara os subdomínios exclusivos de cada ferramenta e exibe os resultados.
func compareUniqueDomains(toolFiles map[string]string) {
	allDomains := make(map[string]bool)
	toolUniqueCounts := make(map[string]int)

	for tool, file := range toolFiles {
		if _, err := os.Stat(file); err == nil {
			data, err := ioutil.ReadFile(file)
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

	// Ordena as ferramentas por quantidade decrescente de subdomínios exclusivos
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

// discovery é a função principal que orquestra a enumeração, agregação e comparação dos subdomínios.
func discovery(domain string, folderName string, compare bool, tools string) {
	baseDir := folderName
	subdomainsDir := filepath.Join(baseDir, "subdomains")
	os.MkdirAll(subdomainsDir, os.ModePerm)

	masterFile := filepath.Join(subdomainsDir, "subdomains.txt")
	oldGlobal := make(map[string]bool)
	if _, err := os.Stat(masterFile); err == nil {
		data, err := ioutil.ReadFile(masterFile)
		if err == nil {
			scanner := bufio.NewScanner(strings.NewReader(string(data)))
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line != "" {
					oldGlobal[line] = true
				}
			}
		}
	}
	oldGlobalCount := len(oldGlobal)

	// Define os comandos para cada ferramenta, injetando o domínio e variáveis de ambiente
	chaosKey := os.Getenv("CHAOS")
	vtAPIKey := os.Getenv("VT_API_KEY")
	toolCommands := map[string]string{
		"subfinder":    fmt.Sprintf("subfinder -d %s -all -silent | grep -i %s", domain, domain),
		"subdominator": fmt.Sprintf("subdominator -d %s -s | grep -i %s", domain, domain),
		"assetfinder":  fmt.Sprintf("assetfinder %s | grep -i %s", domain, domain),
		"findomain":    fmt.Sprintf("findomain --target %s -q | grep -i %s", domain, domain),
		"chaos":        fmt.Sprintf("chaos -d %s -silent -key %s | grep -i %s", domain, chaosKey, domain),
		"virustotal": fmt.Sprintf("bash -c 'url=\"https://www.virustotal.com/api/v3/domains/%s/subdomains?limit=40\"; "+
			"while [ -n \"$url\" ]; do response=$(curl -s \"$url\" -H \"x-apikey: %s\"); "+
			"echo \"$response\" | jq -r \".data[].id\"; "+
			"url=$(echo \"$response\" | jq -r \".links.next // empty\"); done' | grep -i %s", domain, vtAPIKey, domain),
		"shrewdeye":   fmt.Sprintf("curl -s 'https://shrewdeye.app/domains/%s.txt' | grep -i %s | egrep -v '<|>'", domain, domain),
		"shodan":      fmt.Sprintf("curl -s 'https://www.shodan.io/domain/%s' | egrep -i '<li>.+</li>' | awk -F '<li>' '{print $2}' | awk -F '</li>' '{print $1}' | sed 's/$/.%s/' | grep -i %s", domain, domain, domain),
		"crtsh":       fmt.Sprintf("curl -s 'https://crt.sh/?q=%%25.%s&output=json' | jq -r 'map(select(.name_value != null)) | .[].name_value' | sed 's/\\*\\.//g' | tr '[:upper:]' '[:lower:]' | sort -u | grep -i %s", domain, domain),
		"certspotter": fmt.Sprintf("curl -s 'https://api.certspotter.com/v1/issuances?domain=%s&include_subdomains=true&expand=dns_names' | jq -r '.[].dns_names[]' | sort -u | grep -i %s", domain, domain),
	}
	toolFiles := make(map[string]string)
	for tool := range toolCommands {
		toolFiles[tool] = filepath.Join(subdomainsDir, fmt.Sprintf("%s.txt", tool))
	}

	// Se o domínio não for fornecido, mas o parâmetro -c (compare) for usado
	if domain == "" {
		if compare {
			compareUniqueDomains(toolFiles)
		} else {
			red := color.New(color.FgRed).SprintFunc()
			fmt.Println(red("Error: You must provide a domain (-d) or use -c to compare existing results."))
		}
		return
	}

	// Seleciona as ferramentas a serem executadas (todas se não especificado)
	var selectedTools []string
	if tools == "" {
		for tool := range toolCommands {
			selectedTools = append(selectedTools, tool)
		}
	} else {
		parts := strings.Split(tools, ",")
		for _, part := range parts {
			tool := strings.TrimSpace(part)
			selectedTools = append(selectedTools, tool)
		}
	}

	// Executa cada ferramenta em uma goroutine
	var wg sync.WaitGroup
	for _, tool := range selectedTools {
		if cmd, ok := toolCommands[tool]; ok {
			wg.Add(1)
			go func(tool, cmd string) {
				defer wg.Done()
				cyan := color.New(color.FgCyan).SprintFunc()
				fmt.Printf("\n%s Running %s...\n", cyan(""), strings.Title(tool))
				newCount, newFound, err := runTool(cmd, tool, toolFiles[tool])
				if err != nil {
					red := color.New(color.FgRed).SprintFunc()
					fmt.Println(red(err.Error()))
				} else {
					printStat(tool, newCount, newFound)
				}
			}(tool, cmd)
		} else {
			red := color.New(color.FgRed).SprintFunc()
			fmt.Printf("%s Invalid tool: %s\n", red(""), tool)
		}
	}
	wg.Wait()

	// Remove duplicatas individuais em cada arquivo
	for _, file := range toolFiles {
		if _, err := os.Stat(file); err == nil {
			if err := sortUniqueFile(file); err != nil {
				fmt.Printf("Error sorting file %s: %v\n", file, err)
			}
		}
	}

	// Agrega os resultados dos arquivos individuais no arquivo mestre "subdomains.txt"
	cyan := color.New(color.FgCyan).SprintFunc()
	fmt.Printf("\n%s Aggregating results...\n", cyan(""))
	fileList := []string{}
	for _, file := range toolFiles {
		if _, err := os.Stat(file); err == nil {
			fileList = append(fileList, file)
		}
	}
	aggregateCmd := fmt.Sprintf("cat %s | anew %s", strings.Join(fileList, " "), masterFile)
	if err := runShellCommand(aggregateCmd); err != nil {
		fmt.Printf("Error aggregating results: %v\n", err)
	}
	if err := sortUniqueFile(masterFile); err != nil {
		fmt.Printf("Error sorting master file: %v\n", err)
	}

	newGlobal := make(map[string]bool)
	if _, err := os.Stat(masterFile); err == nil {
		data, err := ioutil.ReadFile(masterFile)
		if err == nil {
			scanner := bufio.NewScanner(strings.NewReader(string(data)))
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line != "" {
					newGlobal[line] = true
				}
			}
		}
	}
	totalGlobal := len(newGlobal)
	additionalGlobal := totalGlobal - oldGlobalCount
	green := color.New(color.FgGreen, color.Bold).SprintFunc()
	fmt.Printf("%s[TOTAL] Unique Subdomains: %d (Previously: %d, New: %d)\n", green(""), totalGlobal, oldGlobalCount, additionalGlobal)

	// Filtra os arquivos individuais para manter somente os resultados exclusivos
	fmt.Printf("\n%s Filtering original tool outputs to keep only truly exclusive entries...\n", cyan(""))
	filterUniquePerTool(toolFiles)

	if compare {
		compareUniqueDomains(toolFiles)
	}
}

func main() {
	domain := flag.String("d", "", "Target domain")
	folderName := flag.String("f", "", "Output folder name")
	compare := flag.Bool("c", false, "Compare unique subdomains found per tool")
	tools := flag.String("t", "", "Run specific tool(s), comma-separated (e.g., subfinder,assetfinder)")
	flag.Parse()

	if *folderName == "" {
		fmt.Println("Error: -f (folder-name) is required")
		os.Exit(1)
	}

	printBanner()
	discovery(*domain, *folderName, *compare, *tools)
}
