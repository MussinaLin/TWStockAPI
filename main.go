package main

import (
	"cmp"
	"log/slog"
	"net/http"
	"os"

	"main/db"
	"main/routers"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	sloggin "github.com/samber/slog-gin"
)

func main() {
	_ = godotenv.Load()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := db.InitPool(); err != nil {
		logger.Error("Failed to init database pool", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer db.ClosePool()

	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.Use(sloggin.New(logger))
	r.Use(gin.Recovery())

	// Health checks
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/health/db", func(c *gin.Context) {
		if err := db.Pool().Ping(c.Request.Context()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"detail": err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// API routes
	api := r.Group("/api")
	routers.RegisterStocks(api)
	routers.RegisterDaily(api)
	routers.RegisterAlpha(api)
	routers.RegisterTrade(api)

	port := cmp.Or(os.Getenv("PORT"), "8080")
	logger.Info("Server starting", slog.String("port", port))
	r.Run(":" + port)
}
