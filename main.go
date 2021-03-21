package main

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type User struct {
	gorm.Model
	Accounts []Account
}

type Account struct {
	gorm.Model
	UserID      uint   `gorm:"uniqueIndex:idx_acnt"`
	AssetTicker string `gorm:"uniqueIndex:idx_acnt"`
	Asset       Asset
	Amount      uint `gorm:"default:0"`
}

type Order struct {
	gorm.Model
	BuyerID    string
	Buyer      Account
	BuyAmount  uint
	SellerID   uint
	Seller     Account
	SellAmount uint
}

type Asset struct {
	Ticker string `gorm:"primaryKey;size:3"`
}

type Taker struct {
	UserID  uint `form:"user"`
	OrderID uint `form:"order"`
}

type Maker struct {
	UserID     uint   `form:"user"`
	BuyTicker  string `form:"buyTicker"`
	BuyAmount  uint   `form:"buyAmount"`
	SellTicker string `form:"sellTicker"`
	SellAmount uint   `form:"sellAmount"`
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
	db.AutoMigrate(&User{}, &Account{}, &Asset{}, &Order{})
	r := gin.Default()
	r.LoadHTMLGlob("templates/*.tmpl.html")
	r.GET("/", func(c *gin.Context) {
		var assets []Asset
		if err := db.Find(&assets).Error; err != nil &&
			!errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		} else {
			c.HTML(http.StatusOK, "index.tmpl.html", gin.H{"assets": assets})
		}
	})
	r.POST("/newAsset", func(c *gin.Context) {
		ticker := c.PostForm("ticker")
		if err := db.Create(&Asset{Ticker: ticker}).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err})
		} else {
			c.Status(http.StatusCreated)
		}
	})
	r.POST("/removeAsset", func(c *gin.Context) {
		if err := db.Delete(&Asset{Ticker: c.PostForm("ticker")}); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err})
		} else {
			c.Status(http.StatusNoContent)
		}
	})
	insufficientFund := errors.New("insufficient fund")
	r.POST("/makeOrder", func(c *gin.Context) {
		var maker Maker
		if err := c.ShouldBind(&maker); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err})
		} else if err := db.Transaction(func(tx *gorm.DB) error {
			seller := Account{
				UserID:      maker.UserID,
				AssetTicker: maker.SellTicker,
			}
			if err := tx.Where(seller).Take(&seller).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return insufficientFund
				} else {
					return err
				}
			} else if seller.Amount < maker.SellAmount {
				return insufficientFund
			} else {
				buyer := Account{
					UserID:      maker.UserID,
					AssetTicker: maker.BuyTicker,
				}
				if err := db.Where(buyer).Take(&buyer).
					Error; errors.Is(err, gorm.ErrRecordNotFound) {
					if err := tx.Create(&buyer).Error; err != nil {
						return err
					}
				} else if err != nil {
					return err
				}
				seller.Amount -= maker.SellAmount
				if err := tx.Save(&seller).Error; err != nil {
					return err
				} else {
					return tx.Create(&Order{
						Buyer:      buyer,
						BuyAmount:  maker.BuyAmount,
						Seller:     seller,
						SellAmount: maker.SellAmount,
					}).Error
				}
			}
		}); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": err})
			} else if errors.Is(err, insufficientFund) {
				c.JSON(http.StatusPaymentRequired, gin.H{"error": err})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err})
			}
		}
	})
	r.POST("/takeOrder", func(c *gin.Context) {
		var taker Taker
		if err := c.ShouldBind(&taker); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err})
		} else if err := db.Transaction(func(tx *gorm.DB) error {
			var order Order
			if err := db.Take(&order, taker.OrderID).Error; err != nil {
				return err
			} else {
				var buyFrom Account
				if err := db.Where(&Account{
					UserID:      taker.UserID,
					AssetTicker: order.Buyer.AssetTicker}).
					Take(&buyFrom).Error; err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						return insufficientFund
					} else {
						return err
					}
				} else if buyFrom.Amount < order.BuyAmount {
					return insufficientFund
				} else {
					sellTo := Account{
						UserID:      taker.UserID,
						AssetTicker: order.Seller.AssetTicker,
					}
					if err := db.Where(sellTo).Take(&sellTo).
						Error; errors.Is(err, gorm.ErrRecordNotFound) {
						if err := tx.Create(&sellTo).Error; err != nil {
							return err
						}
					} else if err != nil {
						return err
					}
					sellTo.Amount += order.SellAmount
					buyFrom.Amount -= order.BuyAmount
					order.Buyer.Amount += order.BuyAmount
					if err := tx.Save(&sellTo).Error; err != nil {
						return err
					} else if err := tx.Save(&buyFrom).Error; err != nil {
						return err
					} else if err := tx.Save(&order.Buyer).Error; err != nil {
						return err
					} else if err := tx.Delete(&order).Error; err != nil {
						return err
					} else {
						return nil
					}
				}
			}
		}); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": err})
			} else if errors.Is(err, insufficientFund) {
				c.JSON(http.StatusPaymentRequired, gin.H{"error": err})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err})
			}
		} else {
			c.Status(http.StatusNoContent)
		}
	})
	r.Run(":" + port)
}
