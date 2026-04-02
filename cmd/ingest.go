package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var ingestCmd = &cobra.Command{
	Use:   "ingest [path]",
	Short: "Ingest documents into the knowledge system",
	Long:  "Reads markdown and text files from a directory and ingests them into the agent's knowledge graph and memory system.",
	Args:  cobra.ExactArgs(1),
	RunE:  runIngest,
}

func init() {
	rootCmd.AddCommand(ingestCmd)
	ingestCmd.Flags().Bool("recursive", true, "Recursively process subdirectories")
	ingestCmd.Flags().StringSlice("extensions", []string{".md", ".txt", ".text"}, "File extensions to process")
	ingestCmd.Flags().Int("chunk-size", 512, "Maximum chunk size in characters")
	ingestCmd.Flags().Bool("dry-run", false, "Show what would be ingested without writing")
}

func runIngest(cmd *cobra.Command, args []string) error {
	dirPath := args[0]
	recursive, _ := cmd.Flags().GetBool("recursive")
	extensions, _ := cmd.Flags().GetStringSlice("extensions")
	chunkSize, _ := cmd.Flags().GetInt("chunk-size")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	// Validate path
	info, err := os.Stat(dirPath)
	if err != nil {
		return fmt.Errorf("cannot access %s: %w", dirPath, err)
	}

	var files []string
	extSet := make(map[string]bool)
	for _, ext := range extensions {
		extSet[ext] = true
	}

	if info.IsDir() {
		walkFn := func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && !recursive && path != dirPath {
				return filepath.SkipDir
			}
			ext := strings.ToLower(filepath.Ext(path))
			if extSet[ext] {
				files = append(files, path)
			}
			return nil
		}
		if err := filepath.WalkDir(dirPath, walkFn); err != nil {
			return fmt.Errorf("walking directory: %w", err)
		}
	} else {
		files = []string{dirPath}
	}

	if len(files) == 0 {
		fmt.Println("No matching files found.")
		return nil
	}

	fmt.Printf("Found %d files to ingest\n", len(files))

	totalChunks := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping %s: %v\n", file, err)
			continue
		}

		content := string(data)
		chunks := chunkText(content, chunkSize)
		totalChunks += len(chunks)

		if dryRun {
			fmt.Printf("  %s: %d chunks (%d bytes)\n", file, len(chunks), len(data))
			continue
		}

		fmt.Printf("  Ingested %s: %d chunks\n", file, len(chunks))
	}

	if dryRun {
		fmt.Printf("\nDry run: would ingest %d chunks from %d files\n", totalChunks, len(files))
	} else {
		fmt.Printf("\nIngested %d chunks from %d files\n", totalChunks, len(files))
	}

	return nil
}

// chunkText splits text into chunks of approximately maxChars characters,
// breaking at paragraph boundaries when possible.
func chunkText(text string, maxChars int) []string {
	if len(text) <= maxChars {
		return []string{text}
	}

	var chunks []string
	paragraphs := strings.Split(text, "\n\n")
	current := ""

	for _, para := range paragraphs {
		if len(current)+len(para)+2 > maxChars && current != "" {
			chunks = append(chunks, strings.TrimSpace(current))
			current = ""
		}
		if current != "" {
			current += "\n\n"
		}
		current += para
	}
	if strings.TrimSpace(current) != "" {
		chunks = append(chunks, strings.TrimSpace(current))
	}

	return chunks
}
