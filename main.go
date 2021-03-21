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
	Name string `gorm:"unique;not null"`
}

type Asset struct {
	Ticker string `gorm:"primaryKey;size:3"`
}

type Account struct {
	gorm.Model
	UserID      uint `gorm:"uniqueIndex:idx_acnt"`
	User        User
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

type Deposit struct {
	UserID uint   `form:"user"`
	Ticker string `form:"ticker"`
	Amount uint   `form:"amount"`
}

type Cancel struct {
	OrderID uint `form:"order"`
}

type ListAccount struct {
	UserID uint `form:"user"`
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
		var users []User
		if err := db.Find(&assets).Error; err != nil &&
			!errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusInternalServerError, err)
		} else if err := db.Find(&users).Error; err != nil &&
			!errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusInternalServerError, err)
		} else {
			c.HTML(http.StatusOK, "index.tmpl.html", gin.H{
				"assets": assets,
				"users":  users,
			})
		}
	})
	r.GET("/listAccount", func(c *gin.Context) {
		var listAccount ListAccount
		if c.Bind(&listAccount) == nil {
			var accounts []Account
			if err := db.Find(&accounts, "user_id", listAccount.UserID).
				Error; err != nil {
				c.JSON(http.StatusInternalServerError, err)
			} else {
				c.JSON(http.StatusOK, accounts)
			}
		}
	})
	r.POST("/newAsset", func(c *gin.Context) {
		ticker := c.PostForm("ticker")
		if ticker == "" {
			c.JSON(http.StatusBadRequest, "Ticker is required")
		} else if err := db.Create(Asset{Ticker: ticker}).Error; err != nil {
			c.JSON(http.StatusBadRequest, err)
		} else {
			c.HTML(http.StatusOK, "newAsset.tmpl.html", gin.H{"ticker": ticker})
		}
	})
	r.POST("/newUser", func(c *gin.Context) {
		user := User{Name: c.PostForm("name")}
		if err := db.Create(&user).Error; err != nil {
			c.JSON(http.StatusBadRequest, err)
		} else {
			c.JSON(http.StatusOK, user)
		}
	})
	r.POST("/deposit", func(c *gin.Context) {
		var deposit Deposit
		if c.Bind(&deposit) == nil {
			if err := db.Transaction(func(tx *gorm.DB) error {
				account := Account{UserID: deposit.UserID,
					AssetTicker: deposit.Ticker}
				if err := tx.Take(&account, account).
					Error; errors.Is(err, gorm.ErrRecordNotFound) {
					if err := tx.Create(&account).Error; err != nil {
						return err
					}
				} else if err != nil {
					return err
				}
				account.Amount += deposit.Amount
				if err := tx.Save(&account).Error; err != nil {
					return err
				} else {
					c.JSON(http.StatusOK, account)
					return nil
				}
			}); err != nil {
				c.JSON(http.StatusInternalServerError, err)
			}
		}
	})
	r.POST("/cancel", func(c *gin.Context) {
		var cancel Cancel
		if c.Bind(&cancel) == nil {
			order := Order{Model: gorm.Model{ID: cancel.OrderID}}
			if err := db.Delete(&order).
				Error; errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, err)
			} else if err != nil {
				c.JSON(http.StatusInternalServerError, err)
			} else {
				c.HTML(http.StatusOK, "cancel.tmpl.html",
					gin.H{"order": []Order{order}})
			}
		}
	})
	insufficientFund := errors.New("insufficient fund")
	r.POST("/withdraw", func(c *gin.Context) {
		var withdraw Deposit
		if c.Bind(&withdraw) == nil {
			if err := db.Transaction(func(tx *gorm.DB) error {
				account := Account{UserID: withdraw.UserID,
					AssetTicker: withdraw.Ticker}
				if err := tx.Take(&account, account).
					Error; errors.Is(err, gorm.ErrRecordNotFound) {
					return insufficientFund
				} else if err != nil {
					return err
				} else if account.Amount < withdraw.Amount {
					return insufficientFund
				} else {
					account.Amount -= withdraw.Amount
					if err := tx.Save(&account).Error; err != nil {
						return err
					} else {
						c.JSON(http.StatusOK, account)
						return nil
					}
				}
			}); err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					c.JSON(http.StatusNotFound, err)
				} else if errors.Is(err, insufficientFund) {
					c.JSON(http.StatusPaymentRequired, err)
				} else {
					c.JSON(http.StatusInternalServerError, err)
				}
			}
		}
	})
	r.POST("/makeOrder", func(c *gin.Context) {
		var maker Maker
		if c.Bind(&maker) == nil {
			if err := db.Transaction(func(tx *gorm.DB) error {
				seller := Account{
					UserID:      maker.UserID,
					AssetTicker: maker.SellTicker,
				}
				if err := tx.Take(&seller, seller).Error; err != nil {
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
					if err := db.Take(&buyer, buyer).
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
						order := &Order{
							Buyer:      buyer,
							BuyAmount:  maker.BuyAmount,
							Seller:     seller,
							SellAmount: maker.SellAmount,
						}
						if err := tx.Create(&order).Error; err != nil {
							return err
						} else {
							c.JSON(http.StatusOK, order)
							return nil
						}
					}
				}
			}); err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					c.JSON(http.StatusNotFound, err)
				} else if errors.Is(err, insufficientFund) {
					c.JSON(http.StatusPaymentRequired, err)
				} else {
					c.JSON(http.StatusInternalServerError, err)
				}
			}
		}
	})
	r.POST("/takeOrder", func(c *gin.Context) {
		var taker Taker
		if c.Bind(&taker) == nil {
			if err := db.Transaction(func(tx *gorm.DB) error {
				var order Order
				if err := db.Take(&order, taker.OrderID).Error; err != nil {
					return err
				} else {
					buyFrom := Account{
						UserID:      taker.UserID,
						AssetTicker: order.Buyer.AssetTicker,
					}
					if err := db.Take(&buyFrom, buyFrom).Error; err != nil {
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
						if err := db.Take(&sellTo, sellTo).
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
							c.JSON(http.StatusOK, sellTo)
							return nil
						}
					}
				}
			}); err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					c.JSON(http.StatusNotFound, err)
				} else if errors.Is(err, insufficientFund) {
					c.JSON(http.StatusPaymentRequired, err)
				} else {
					c.JSON(http.StatusInternalServerError, err)
				}
			}
		}
	})
	r.Run(":" + port)
}
