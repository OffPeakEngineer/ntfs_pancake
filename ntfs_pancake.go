package main

import (
    "bytes"
    "compress/flate"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "runtime"
    "sync"
    "syscall"
    "unsafe"

    "golang.org/x/sys/windows"
)

const (
    FSCTL_SET_COMPRESSION          = 0x9C040
    COMPRESSION_FORMAT_DEFAULT     = 1
    COMPRESSION_FORMAT_NONE        = 0
    COMPRESSION_EFFICIENCY_THRESHOLD = 10 // 10% minimum space saving threshold
    WORKER_COUNT = 200 // Number of concurrent workers
)

var (
    totalFilesProcessed int
    totalFilesCompressed int
    totalFilesDecompressed int
    totalSpaceSaved int64
    mu sync.Mutex
)

func enableCompression(path string) error {
    return setCompression(path, COMPRESSION_FORMAT_DEFAULT)
}

func disableCompression(path string) error {
    return setCompression(path, COMPRESSION_FORMAT_NONE)
}

func setCompression(path string, compressionFormat uint16) error {
    // Open the file or directory
    file, err := syscall.CreateFile(
        syscall.StringToUTF16Ptr(path),
        syscall.GENERIC_READ | syscall.GENERIC_WRITE,
        syscall.FILE_SHARE_READ | syscall.FILE_SHARE_WRITE,
        nil,
        syscall.OPEN_EXISTING,
        syscall.FILE_FLAG_BACKUP_SEMANTICS,
        0,
    )
    if err != nil {
        return err
    }
    defer syscall.CloseHandle(file)

    // Set the compression state
    var bytesReturned uint32
    err = windows.DeviceIoControl(
        windows.Handle(file),
        FSCTL_SET_COMPRESSION,
        (*byte)(unsafe.Pointer(&compressionFormat)),
        uint32(unsafe.Sizeof(compressionFormat)),
        nil,
        0,
        &bytesReturned,
        nil,
    )
    if err != nil {
        return err
    }

    return nil
}

func compressFileInMemory(path string) (int64, int64, error) {
    originalFile, err := os.Open(path)
    if err != nil {
        return 0, 0, err
    }
    defer originalFile.Close()

    var originalSize int64
    var compressedSize int64

    // Create a buffer to hold the compressed data
    var compressedBuffer bytes.Buffer

    // Create a flate writer with default compression level
    writer, err := flate.NewWriter(&compressedBuffer, flate.DefaultCompression)
    if err != nil {
        return 0, 0, err
    }
    defer writer.Close()

    // Copy the original file data to the flate writer
    buf := make([]byte, 4096)
    for {
        n, err := originalFile.Read(buf)
        if err != nil && err != io.EOF {
            return 0, 0, err
        }
        if n == 0 {
            break
        }
        originalSize += int64(n)
        if _, err := writer.Write(buf[:n]); err != nil {
            return 0, 0, err
        }
    }

    // Close the writer to flush any remaining data
    if err := writer.Close(); err != nil {
        return 0, 0, err
    }

    // Get the compressed size
    compressedSize = int64(compressedBuffer.Len())

    return originalSize, compressedSize, nil
}

func processFile(path string) {
    // Get the system memory info
    var memStat runtime.MemStats
    runtime.ReadMemStats(&memStat)

    // Check if the file can fit into the available memory
    originalSize, err := getFileSize(path)
    if err != nil {
        fmt.Printf("Error getting file size for %s: %v\n", path, err)
        return
    }
    if originalSize > int64(memStat.Frees) {
        fmt.Printf("File %s is too large to fit into available memory. Skipping...\n", path)
        return
    }

    // Compress the file in memory
    originalSize, compressedSize, err := compressFileInMemory(path)
    if err != nil {
        fmt.Printf("Error compressing file in memory %s: %v\n", path, err)
        return
    }

    // Calculate space savings
    spaceSaved := originalSize - compressedSize
    savingRatio := float64(spaceSaved) / float64(originalSize) * 100

    mu.Lock()
    totalFilesProcessed++
    // Check if compression is worth it
    if savingRatio < COMPRESSION_EFFICIENCY_THRESHOLD {
        fmt.Printf("Compression not worth it for %s, saving ratio: %.2f%%. Disabling compression...\n", path, savingRatio)
        err = disableCompression(path)
        if err != nil {
            fmt.Printf("Error disabling compression for %s: %v\n", path, err)
        } else {
            totalFilesDecompressed++
        }
    } else {
        fmt.Printf("Compression beneficial for %s, saving ratio: %.2f%%. Enabling compression...\n", path, savingRatio)
        err = enableCompression(path)
        if err != nil {
            fmt.Printf("Error enabling compression for %s: %v\n", path, err)
        } else {
            totalFilesCompressed++
            totalSpaceSaved += spaceSaved
        }
    }
    mu.Unlock()
}

func getFileSize(path string) (int64, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
			return 0, err
	}
	return fileInfo.Size(), nil
}

func worker(paths <-chan string, wg *sync.WaitGroup) {
    defer wg.Done()
    for path := range paths {
        processFile(path)
    }
}

func scanAndCompressFolder(root string) {
    paths := make(chan string)
    var wg sync.WaitGroup

    // Start workers
    for i := 0; i < WORKER_COUNT; i++ {
        wg.Add(1)
        go worker(paths, &wg)
    }

    // Walk through the folder and send file paths to the channel
    go func() {
        defer close(paths)
        err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
            if err != nil {
                fmt.Printf("Error accessing path %s: %v\n", path, err)
                return err
            }

            // Only process normal files
            if !info.IsDir() && info.Mode().IsRegular() {
                paths <- path
            }

            return nil
        })

        if err != nil {
            fmt.Printf("Error scanning folder %s: %v\n", root, err)
        }
    }()

    // Wait for all workers to finish
    wg.Wait()
}

func main() {
    if len(os.Args) != 2 {
        fmt.Printf("Usage: %s <folder path>\n", os.Args[0])
        return
    }

    folderPath := os.Args[1]
    scanAndCompressFolder(folderPath)

    // Print summary
    fmt.Printf("\nSummary:\n")
    fmt.Printf("Total files processed: %d\n", totalFilesProcessed)
    fmt.Printf("Total files compressed: %d\n", totalFilesCompressed)
    fmt.Printf("Total files decompressed: %d\n", totalFilesDecompressed)
    fmt.Printf("Total space saved: %d bytes\n", totalSpaceSaved)
}
