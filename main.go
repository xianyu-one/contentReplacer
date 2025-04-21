package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

type fileJob struct {
	path    string
	find    string
	replace string
}

type processResult struct {
	filePath string // 处理的文件路径
	updated  bool   // 文件是否被更新
	err      error  // 处理过程中遇到的错误
}

func worker(id int, wg *sync.WaitGroup, jobs <-chan fileJob, results chan<- processResult) {
	defer wg.Done() // 确保在 worker 退出时通知 WaitGroup
	// fmt.Printf("Worker %d starting\n", id) // 可以取消注释以查看 worker 启动
	for job := range jobs {
		// fmt.Printf("Worker %d processing file: %s\n", id, job.path) // 可选：打印处理信息
		updated, err := processFile(job.path, job.find, job.replace)
		// 将处理结果（包括是否更新、文件路径和错误）发送回去
		results <- processResult{filePath: job.path, updated: updated, err: err}
	}
	// fmt.Printf("Worker %d finished\n", id) // 可以取消注释以查看 worker 结束
}

func processFile(path, find, replace string) (bool, error) {
	// 使用 os.ReadFile 替换 ioutil.ReadFile
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("failed to read file: %w", err)
	}

	content := string(data)
	// 只有在内容确实包含查找字符串时才继续
	if strings.Contains(content, find) {
		newContent := strings.ReplaceAll(content, find, replace)
		// 只有在内容确实发生改变时才写入
		if newContent != content {
			// 获取文件原始权限
			info, err := os.Stat(path)
			if err != nil {
				return false, fmt.Errorf("failed to get file info: %w", err)
			}
			// 使用 os.WriteFile 替换 ioutil.WriteFile，并保留原始权限
			err = os.WriteFile(path, []byte(newContent), info.Mode().Perm())
			if err != nil {
				return false, fmt.Errorf("failed to write file: %w", err)
			}
			// 文件已更新，返回 true
			return true, nil
		}
	}
	return false, nil
}

func main() {
	// --- 参数解析 ---
	rootDir := flag.String("p", ".", "Specify target folder (指定目标文件夹)")
	extensions := flag.String("fe", "md,txt", "Specify file extensions (指定文件扩展名，逗号分隔)")
	find := flag.String("r", "", "Specify the original content to find (指定要查找的原始内容)")
	replace := flag.String("t", "", "Specify the content to replace with (指定要替换的新内容)")
	numWorkers := flag.Int("w", runtime.NumCPU(), "Number of worker goroutines (指定工作 goroutine 数量，默认为 CPU 核心数)")
	flag.Parse()

	if *find == "" {
		log.Fatal("Error: find content (-r) cannot be empty (错误：查找内容 (-r) 不能为空)")
	}

	exts := strings.Split(*extensions, ",")
	// 创建一个 map 用于快速查找扩展名，并确保它们以 "." 开头
	validExtensions := make(map[string]struct{})
	for _, ext := range exts {
		trimmedExt := strings.TrimSpace(ext)
		if trimmedExt != "" {
			validExtensions["."+trimmedExt] = struct{}{}
		}
	}
	if len(validExtensions) == 0 {
		log.Fatal("Error: No valid file extensions specified (-fe) (错误：未指定有效的文件扩展名 (-fe))")
	}

	// --- 并发设置 ---
	var wg sync.WaitGroup
	jobs := make(chan fileJob, *numWorkers*2) // 文件任务 channel
	results := make(chan processResult, 100)  // 处理结果 channel (包含错误和更新状态)
	var updatedFiles []string                 // 用于存储已更新的文件路径
	var processedFileCount int                // 统计处理的文件总数
	var errorOccurred bool                    // 标记是否发生错误
	var walkErr error                         // 存储遍历过程中的错误

	fmt.Printf("Starting %d workers...\n", *numWorkers)
	// 启动 worker goroutines
	for w := 1; w <= *numWorkers; w++ {
		wg.Add(1)
		go worker(w, &wg, jobs, results)
	}

	// --- 启动结果收集 goroutine ---
	// 这个 goroutine 单独运行，负责从 results channel 读取处理结果
	// 使用一个新的 WaitGroup 来确保这个 goroutine 在主程序退出前处理完所有结果
	var resultsWg sync.WaitGroup
	resultsWg.Add(1)
	go func() {
		defer resultsWg.Done()
		for result := range results {
			processedFileCount++ // 每收到一个结果，增加处理计数
			if result.err != nil {
				log.Printf("Error processing %s: %v\n", result.filePath, result.err)
				errorOccurred = true // 标记发生了错误
			} else if result.updated {
				// 如果文件成功更新，将其路径添加到列表中
				updatedFiles = append(updatedFiles, result.filePath)
			}
		}
	}()

	// --- 文件遍历 ---
	// 使用 WalkDir 遍历目录
	// 在单独的 goroutine 中执行遍历，并将遍历错误存储起来
	go func() {
		// WalkDir 完成后关闭 jobs channel，通知 worker 没有更多任务
		defer close(jobs)
		walkErr = filepath.WalkDir(*rootDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				// 如果 WalkDir 本身遇到错误（如权限问题），记录并可能停止遍历
				log.Printf("Error accessing path %q: %v\n", path, err)
				// 根据需要决定是返回 err 停止遍历，还是返回 nil 继续（如果允许部分成功）
				// 这里选择返回错误，停止遍历
				return err
			}
			// 跳过目录，只处理文件
			if !d.IsDir() {
				// 检查文件扩展名是否匹配
				fileExt := filepath.Ext(path)
				if _, ok := validExtensions[fileExt]; ok {
					// 将符合条件的文件路径发送到 jobs channel
					jobs <- fileJob{path: path, find: *find, replace: *replace}
				}
			}
			return nil // 继续遍历
		})
	}()

	// --- 等待与收尾 ---
	fmt.Println("Waiting for workers to finish processing files...")
	wg.Wait() // 等待所有 worker 完成文件处理任务 (即 jobs channel 被耗尽)
	fmt.Println("All workers finished processing.")

	// 关闭 results channel，通知结果收集 goroutine 没有更多结果需要处理
	// 必须在 wg.Wait() 之后关闭，确保所有 worker 都已停止向 results 发送数据
	close(results)

	// 等待结果收集 goroutine 处理完所有缓冲在 results channel 中的结果
	fmt.Println("Waiting for results collection to finish...")
	resultsWg.Wait()
	fmt.Println("Results collection finished.")

	// --- 输出最终结果 ---
	fmt.Println("\n--- Summary ---")
	fmt.Printf("Total files processed: %d\n", processedFileCount)

	// 检查遍历过程中是否发生错误
	if walkErr != nil {
		log.Printf("Error occurred during directory walk: %v\n", walkErr)
		errorOccurred = true
	}

	// 打印更新的文件列表
	if len(updatedFiles) > 0 {
		fmt.Printf("Files updated (%d):\n", len(updatedFiles))
		for _, path := range updatedFiles {
			fmt.Printf("  - %s\n", path) // 稍微缩进，更清晰
		}
	} else {
		fmt.Println("No files were updated.")
	}
	fmt.Println("---------------")

	if errorOccurred {
		fmt.Println("\nSome errors occurred during processing. Please check the logs above.")
	} else {
		fmt.Println("\nOperation completed successfully.")
	}
}
