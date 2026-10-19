package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"svm.rakennus/site/internal/site"
	"svm.rakennus/site/web"
	"syscall"
	"time"
)

func main() {
	app, e := site.New(web.Files)
	if e != nil {
		log.Fatal(e)
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{Addr: ":" + port, Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 45 * time.Second, WriteTimeout: 90 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 1 << 20}
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-quit
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()
	log.Printf("SVM Rakennus site ready on port %s", port)
	if e = srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		log.Fatal(e)
	}
}
