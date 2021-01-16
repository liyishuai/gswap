package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Asset ...
type Asset struct {
	gorm.Model
	Ticker string `gorm:"unique"`
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		log.Fatal("$PORT must be set")
	}
	sqlDB, err := sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("Error opening SQL database: %q", err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{
		Conn: sqlDB,
	}), &gorm.Config{})
	if err != nil {
		log.Fatalf("Error opening Gorm database: %q", err)
	}
	r := gin.New()
	r.Use(gin.Logger())
	r.LoadHTMLGlob("templates/*.tmpl.html")
	r.GET("/", func(c *gin.Context) {
		var assets []Asset
		db.Find(&assets)
		c.HTML(http.StatusOK, "index.tmpl.html", gin.H{"assets": assets})
	})
	r.POST("/newAsset", func(c *gin.Context) {
		db.Create(&Asset{Ticker: c.PostForm("ticker")})
		var assets []Asset
		db.Find(&assets)
		c.HTML(http.StatusOK, "index.tmpl.html", gin.H{"assets": assets})
	})
	r.Run(":" + port)
}
