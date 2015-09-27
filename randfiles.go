package main

import (
	crand "crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/ancientlore/flagcfg"
	"github.com/ancientlore/kubismus"
	"github.com/facebookgo/flagenv"
	"log"
	"math/rand"
	"net/http"
	"os"
	"runtime"
	"time"
)

var (
	addr       string        = ":8081"
	cpus       int           = 1
	minSize    int           = 1024
	maxSize    int           = 8192
	workingDir string        = ""
	threads    int           = 1
	delay      time.Duration = 100 * time.Millisecond
	ext        string        = ".bin"
	help       bool
)

func init() {
	// http service/status address
	flag.StringVar(&addr, "addr", addr, "HTTP service address for monitoring.")

	// runtime
	flag.IntVar(&cpus, "cpu", cpus, "Number of CPUs to use.")
	flag.StringVar(&workingDir, "wd", workingDir, "Set the working directory.")

	// file settings
	flag.IntVar(&threads, "threads", threads, "Number of writer threads.")
	flag.DurationVar(&delay, "delay", delay, "Approximate delay between writes.")
	flag.IntVar(&minSize, "minsize", minSize, "Minimum file size.")
	flag.IntVar(&maxSize, "maxsize", maxSize, "Maximum file size.")
	flag.StringVar(&ext, "ext", ext, "File extension.")

	// help
	flag.BoolVar(&help, "help", false, "Show help.")
}

func showHelp() {
	fmt.Println(`randfiles

Create random files

Usage:
  randfiles [options]

Example:
  randfiles -addr :8081 -delay 1s -maxsize 10000 -minsize 5000 -ext .bin

Options:`)
	flag.PrintDefaults()
	fmt.Println(`
All of the options can be set via environment variables prefixed with "RANDFILES_".

Options can also be specified in a TOML configuration file named "randfiles.config". The location
of the file can be overridden with the RANDFILES_CONFIG environment variable.`)
}

func main() {
	// Parse flags from command-line
	flag.Parse()

	// Parser flags from config
	flagcfg.AddDefaults()
	flagcfg.Parse()

	// Parse flags from environment (using github.com/facebookgo/flagenv)
	flagenv.Prefix = "RANDFILES_"
	flagenv.Parse()

	if help {
		showHelp()
		return
	}

	// setup number of CPUs
	runtime.GOMAXPROCS(cpus)

	name, _ := os.Hostname()

	http.Handle("/", http.HandlerFunc(kubismus.ServeHTTP))

	kubismus.Setup("randfiles", "")
	kubismus.Note("Host Name", name)
	kubismus.Note("CPUs", fmt.Sprintf("%d of %d", runtime.GOMAXPROCS(0), runtime.NumCPU()))
	kubismus.Note("Delay", delay.String())
	kubismus.Note("Writer threads", fmt.Sprintf("%d", threads))
	kubismus.Note("File size", fmt.Sprintf("%d to %d bytes", minSize, maxSize))
	kubismus.Note("File extension", ext)
	kubismus.Define("Data", kubismus.COUNT, "Files/second")
	kubismus.Define("Data", kubismus.SUM, "Bytes/second")

	// switch to working dir
	if workingDir != "" {
		err := os.Chdir(workingDir)
		if err != nil {
			log.Fatal(err)
		}
	}
	wd, err := os.Getwd()
	if err == nil {
		kubismus.Note("Working Directory", wd)
	}

	rand.Seed(time.Now().UnixNano())
	for i := 0; i < threads; i++ {
		go writeFiles(minSize, maxSize, delay, ext)
	}
	go calcMetrics()

	log.Fatal(http.ListenAndServe(addr, nil))
}

func writeFiles(mn, mx int, delay time.Duration, extension string) {
	b := make([]byte, mx)
	fn := make([]byte, 16)
	for {
		_, err := crand.Read(fn)
		if err != nil {
			panic(err)
		}

		// read data
		sz := mn + rand.Intn(mx-mn)
		_, err = crand.Read(b[:sz])
		if err != nil {
			panic(err)
		}

		// create file
		fns := hex.EncodeToString(fn) + extension
		//log.Printf("File named [%s] is %d bytes", fns, sz)
		f, err := os.Create(fns)
		if err != nil {
			log.Print(err)
		} else {
			_, err = f.Write(b[:sz])
			if err != nil {
				log.Print(err)
			}
			err = f.Close()
			if err != nil {
				log.Print(err)
			}
			kubismus.Metric("Data", 1, float64(sz))
		}
		time.Sleep(delay)
	}
}

func calcMetrics() {
	tck := time.NewTicker(time.Duration(10) * time.Second)
	for {
		select {
		case <-tck.C:
			kubismus.Note("Goroutines", fmt.Sprintf("%d", runtime.NumGoroutine()))
			go func() {
				v := kubismus.GetMetrics("Data", kubismus.SUM)
				defer kubismus.ReleaseMetrics(v)
				c := kubismus.GetMetrics("Data", kubismus.COUNT)
				defer kubismus.ReleaseMetrics(c)
				sz := len(c)
				T := 0.0
				C := 0.0
				for i := sz - 60; i < sz; i++ {
					C += c[i]
					T += v[i]
				}
				A := 0.0
				if C > 0.0 {
					A = T / C
				}
				kubismus.Note("Last One Minute", fmt.Sprintf("%.0f Files, %.0f Average Size, %0.f Bytes", C, A, T))
				log.Printf("Last one minute: %.0f Files, %.0f Average Size, %0.f Bytes", C, A, T)
			}()
		}
	}
}
