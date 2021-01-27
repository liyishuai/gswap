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
	Ticker string `gorm:"primaryKey;size:3"`
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
	r := gin.Default()
	r.LoadHTMLGlob("templates/*.tmpl.html")
	r.GET("/", func(c *gin.Context) {
		var assets []Asset
		db.Find(&assets)
		c.HTML(http.StatusOK, "index.tmpl.html", gin.H{"assets": assets})
	})
	r.POST("/newAsset", func(c *gin.Context) {
		ticker := c.PostForm("ticker")
		db.Create(&Asset{Ticker: ticker})
		if db.Error != nil {
			c.HTML(http.StatusBadRequest, "asset.tmpl.html", gin.H{"message": db.Error})
		} else {
			c.HTML(http.StatusOK, "asset.tmpl.html", gin.H{"message": "已发行" + ticker})
		}
	})
	r.POST("/removeAsset", func(c *gin.Context) {
		ticker := c.PostForm("ticker")
		db.Delete(&Asset{}, "ticker = ?", ticker)
		if db.Error != nil {
			c.HTML(http.StatusBadRequest, "asset.tmpl.html", gin.H{"message": db.Error})
		} else {
			c.HTML(http.StatusOK, "asset.tmpl.html", gin.H{"message": "已撤销" + ticker})
		}
	})
	r.Run(":" + port)
}
