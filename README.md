# SFINDER

SFINDER is a subdomain enumeration tool written in GoLang that replicates the functionality of the original Python version by Gilson Oliveira. It features an ASCII banner, colored output, multithreaded execution, and integrates multiple subdomain enumeration tools into one workflow.

## Features

- **ASCII Banner:** Displays an eye-catching banner using `go-figure`.
- **Colored Output:** Uses `fatih/color` to enhance terminal output with colors.
- **Multithreading:** Executes subdomain enumeration tools concurrently using goroutines.
- **Tool Integration:** Supports various subdomain enumeration tools:
  - subfinder
  - subdominator
  - assetfinder
  - findomain
  - chaos
  - virustotal
  - shrewdeye
  - shodan
  - crtsh
  - certspotter
- **Result Aggregation:** Combines results from all tools, removes duplicates, and filters out non-unique entries.
- **Comparison Mode:** Compares unique subdomains discovered by each tool.

## Requirements

- **Go:** Version 1.16 or later.
- **External Utilities:** Ensure the following are installed and available in your PATH:
  - `anew`
  - `grep`
  - `sort`
  - `curl`
  - `jq`
  - `awk`
  - `sed`
- **Environment Variables:**
  - `CHAOS` - API key for the Chaos tool.
  - `VT_API_KEY` - API key for the VirusTotal tool.

## Installation

1. **Clone the Repository:**

   ```bash
   git clone https://github.com/gilsgil/sfinder.git
   cd sfinder
   ```

2. **Install Dependencies:**

   Install the Go packages required by the tool:

   ```bash
   go get github.com/common-nighthawk/go-figure
   go get github.com/fatih/color
   ```

3. **Build the Application:**

   ```bash
   go build -o sfinder main.go

   # OR

   go install -v github.com/gilsgil/sfinder@latest
   ```

## Usage

Run the tool with the following command:

```bash
./sfinder -d <target-domain> -f <output-folder> [options]
```

### Options

- `-d, --domain`  
  Target domain for subdomain enumeration.

- `-f, --folder-name`  
  Output folder name (required).

- `-c, --compare`  
  Compare unique subdomains found per tool.

- `-t, --tools`  
  Comma-separated list of tools to run (e.g., `subfinder,assetfinder`).

### Examples

- **Run all tools for a domain:**

  ```bash
  ./sfinder -d example.com -f output_folder
  ```

- **Run only specific tools (e.g., subfinder and assetfinder):**

  ```bash
  ./sfinder -d example.com -f output_folder -t subfinder,assetfinder
  ```

- **Compare unique subdomains from existing results:**

  ```bash
  ./sfinder -f output_folder -c
  ```

## License

This project is licensed under the [MIT License](LICENSE).

## Contributing

Contributions are welcome! Please fork the repository and submit pull requests. For any issues or feature requests, feel free to open an issue on the GitHub repository.

## Disclaimer

This tool utilizes external utilities and APIs. Ensure you have the necessary permissions and valid API keys before use. The authors and contributors are not responsible for any misuse or damages caused by this tool.
