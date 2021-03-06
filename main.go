package main

import (
	"context"
	"database/sql"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/bradleyfalzon/gopherci/internal/analyser"
	"github.com/bradleyfalzon/gopherci/internal/db"
	"github.com/bradleyfalzon/gopherci/internal/github"
	"github.com/bradleyfalzon/gopherci/internal/queue"
	"github.com/bradleyfalzon/gopherci/internal/web"
	_ "github.com/go-sql-driver/mysql"
	gh "github.com/google/go-github/github"
	"github.com/joho/godotenv"
	"github.com/pkg/errors"
	"github.com/pressly/chi"
	"github.com/pressly/chi/middleware"
	migrate "github.com/rubenv/sql-migrate"
)

func main() {
	// Load environment from .env, ignore errors as it's optional and dev only
	_ = godotenv.Load()

	r := chi.NewRouter()
	r.Use(middleware.RealIP) // Blindly accept XFF header, ensure LB overwrites it
	r.Use(middleware.DefaultCompress)
	r.Use(middleware.Recoverer)
	r.Use(middleware.NoCache)

	// http server for graceful shutdown
	srv := &http.Server{
		Addr:    ":3000",
		Handler: r,
	}

	// Graceful shutdown handler
	ctx, cancel := context.WithCancel(context.Background())
	go SignalHandler(cancel, srv)

	switch {
	case os.Getenv("GCI_BASE_URL") == "":
		log.Println("GCI_BASE_URL is blank, URLs linking back to GopherCI will not work")
	case os.Getenv("GITHUB_ID") == "":
		log.Fatalln("GITHUB_ID is not set")
	case os.Getenv("GITHUB_PEM_FILE") == "":
		log.Fatalln("GITHUB_PEM_FILE is not set")
	case os.Getenv("GITHUB_WEBHOOK_SECRET") == "":
		log.Fatalln("GITHUB_WEBHOOK_SECRET is not set")
	}

	// Database
	log.Printf("Connecting to %q db name: %q, username: %q, host: %q, port: %q",
		os.Getenv("DB_DRIVER"), os.Getenv("DB_DATABASE"), os.Getenv("DB_USERNAME"), os.Getenv("DB_HOST"), os.Getenv("DB_PORT"),
	)

	dsn := fmt.Sprintf(`%s:%s@tcp(%s:%s)/%s?charset=utf8&collation=utf8_unicode_ci&timeout=6s&time_zone='%%2B00:00'&parseTime=true`,
		os.Getenv("DB_USERNAME"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_HOST"), os.Getenv("DB_PORT"), os.Getenv("DB_DATABASE"),
	)

	sqlDB, err := sql.Open(os.Getenv("DB_DRIVER"), dsn)
	if err != nil {
		log.Fatal("Error setting up DB:", err)
	}

	// Do DB migrations
	migrations := &migrate.FileMigrationSource{Dir: "migrations"}
	migrate.SetTable("migrations")
	direction := migrate.Up
	migrateMax := 0
	if len(os.Args) > 1 && os.Args[1] == "down" {
		direction = migrate.Down
		migrateMax = 1
	}
	n, err := migrate.ExecMax(sqlDB, os.Getenv("DB_DRIVER"), migrations, direction, migrateMax)
	log.Printf("Applied %d migrations to database", n)
	if err != nil {
		log.Fatal(errors.Wrap(err, "could not execute all migrations"))
	}

	db, err := db.NewSQLDB(sqlDB, os.Getenv("DB_DRIVER"))
	if err != nil {
		log.Fatalln("could not initialise db:", err)
	}

	// Analyser
	log.Printf("Using analyser %q", os.Getenv("ANALYSER"))
	var analyse analyser.Analyser
	switch os.Getenv("ANALYSER") {
	case "filesystem":
		if os.Getenv("ANALYSER_FILESYSTEM_PATH") == "" {
			log.Fatalln("ANALYSER_FILESYSTEM_PATH is not set")
		}
		analyse, err = analyser.NewFileSystem(os.Getenv("ANALYSER_FILESYSTEM_PATH"))
		if err != nil {
			log.Fatalln("could not initialise file system analyser:", err)
		}
	case "docker":
		image := os.Getenv("ANALYSER_DOCKER_IMAGE")
		if image == "" {
			image = analyser.DockerDefaultImage
		}
		analyse, err = analyser.NewDocker(image)
		if err != nil {
			log.Fatalln("could not initialise Docker analyser:", err)
		}
	case "":
		log.Fatalln("ANALYSER is not set")
	default:
		log.Fatalf("Unknown ANALYSER option %q", os.Getenv("ANALYSER"))
	}

	// GitHub
	log.Printf("GitHub Integration ID: %q, GitHub Integration PEM File: %q", os.Getenv("GITHUB_ID"), os.Getenv("GITHUB_PEM_FILE"))
	integrationID, err := strconv.ParseInt(os.Getenv("GITHUB_ID"), 10, 64)
	if err != nil {
		log.Fatalf("could not parse integrationID %q", os.Getenv("GITHUB_ID"))
	}

	integrationKey, err := ioutil.ReadFile(os.Getenv("GITHUB_PEM_FILE"))
	if err != nil {
		log.Fatalf("could not read private key for GitHub integration: %s", err)
	}

	// queuePush is used to add a job to the queue
	var queuePush = make(chan interface{})

	gh, err := github.New(analyse, db, queuePush, int(integrationID), integrationKey, os.Getenv("GITHUB_WEBHOOK_SECRET"), os.Getenv("GCI_BASE_URL"))
	if err != nil {
		log.Fatalln("could not initialise GitHub:", err)
	}
	r.Post("/gh/webhook", gh.WebHookHandler)
	r.Get("/gh/callback", gh.CallbackHandler)

	var (
		wg         sync.WaitGroup // wait for queue to finish before exiting
		qProcessor = queueProcessor{github: gh}
	)

	switch os.Getenv("QUEUER") {
	case "memory":
		memq := queue.NewMemoryQueue()
		memq.Wait(ctx, &wg, queuePush, qProcessor.Process)
	case "gcppubsub":
		switch {
		case os.Getenv("QUEUER_GCPPUBSUB_PROJECT_ID") == "":
			log.Fatalf("QUEUER_GCPPUBSUB_PROJECT_ID is not set")
		}
		gcp, err := queue.NewGCPPubSubQueue(ctx, os.Getenv("QUEUER_GCPPUBSUB_PROJECT_ID"), os.Getenv("QUEUER_GCPPUBSUB_TOPIC"))
		if err != nil {
			log.Fatal("Could not initialise GCPPubSubQueue:", err)
		}
		gcp.Wait(ctx, &wg, queuePush, qProcessor.Process)
	case "":
		log.Fatalln("QUEUER is not set")
	default:
		log.Fatalf("Unknown QUEUER option %q", os.Getenv("QUEUER"))
	}

	// Web routes
	web, err := web.NewWeb(db, gh)
	if err != nil {
		log.Fatalln("main: error loading web:", err)
	}
	workDir, _ := os.Getwd()
	r.FileServer("/static", http.Dir(filepath.Join(workDir, "internal", "web", "static")))
	r.NotFound(web.NotFoundHandler)
	r.Get("/analysis/:analysisID", web.AnalysisHandler)

	// Health checks
	r.Get("/health-check", HealthCheckHandler)

	// Listen
	log.Println("main: listening on", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Println("main: http server error:", err)
		cancel()
	}

	// Wait for current item in queue to finish
	log.Println("main: waiting for queuer to finish")
	wg.Wait()
	log.Println("main: exiting gracefully")
}

// Queue processor is the callback called by queuer when receiving a job
type queueProcessor struct {
	github *github.GitHub
}

// queueListen listens for jobs on the queue and executes the relevant handlers.
func (q *queueProcessor) Process(job interface{}) {
	start := time.Now()
	log.Printf("queueProcessor: processing job type %T", job)
	var err error
	switch e := job.(type) {
	case *gh.PushEvent:
		err = q.github.Analyse(github.PushConfig(e))
		if err != nil {
			err = errors.Wrapf(err, "cannot analyse push event for sha %v on repo %v", *e.After, *e.Repo.HTMLURL)
		}
	case *gh.PullRequestEvent:
		err = q.github.Analyse(github.PullRequestConfig(e))
		if err != nil {
			err = errors.Wrapf(err, "cannot analyse pr %v", *e.PullRequest.HTMLURL)
		}
	default:
		err = fmt.Errorf("unknown queue job type %T", e)
	}
	log.Printf("queueProcessor: finished processing in %v", time.Since(start))
	if err != nil {
		log.Println("queueProcessor: processing error:", err)
	}
}
