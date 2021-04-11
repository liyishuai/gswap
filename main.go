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
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
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
	BuyerID    uint
	Buyer      Account
	BuyAmount  uint
	SellerID   uint
	Seller     Account
	SellAmount uint
}

type Taker struct {
	UserID  uint `form:"user" binding:"required"`
	OrderID uint `form:"order" binding:"required"`
}

type Maker struct {
	UserID     uint   `form:"user" binding:"required"`
	BuyTicker  string `form:"buyTicker" binding:"required"`
	BuyAmount  uint   `form:"buyAmount" binding:"required"`
	SellTicker string `form:"sellTicker" binding:"required"`
	SellAmount uint   `form:"sellAmount" binding:"required"`
}

type Ticket struct {
	UserID uint   `form:"user" binding:"required"`
	Ticker string `form:"ticker" binding:"required"`
	Amount uint   `form:"amount" binding:"required"`
}

func credit(account *Account, amount uint, tx *gorm.DB) error {
	account.Amount += amount
	if err := tx.Save(&account).Error; err != nil {
		return err
	} else {
		return nil
	}
}

func deposit(dep Ticket, account *Account) func(*gorm.DB) error {
	return func(tx *gorm.DB) error {
		modelAccount := Account{
			UserID:      dep.UserID,
			AssetTicker: dep.Ticker,
		}
		if err := tx.Take(&account, modelAccount).
			Error; errors.Is(err, gorm.ErrRecordNotFound) {
			*account = modelAccount
			if err := tx.Create(&account).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		return credit(account, dep.Amount, tx)
	}
}

var errInsufficientFund = errors.New("insufficient fund")

func debit(account *Account, amount uint, tx *gorm.DB) error {
	if account.Amount < amount {
		return errInsufficientFund
	} else {
		account.Amount -= amount
		if err := tx.Save(&account).Error; err != nil {
			return err
		} else {
			return nil
		}
	}
}

func withdraw(wit Ticket, account *Account) func(*gorm.DB) error {
	return func(tx *gorm.DB) error {
		if err := tx.Take(&account, Account{
			UserID:      wit.UserID,
			AssetTicker: wit.Ticker,
		}).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return errInsufficientFund
		} else if err != nil {
			return err
		} else {
			return debit(account, wit.Amount, tx)
		}
	}
}

type ListAccount struct {
	UserID uint `form:"user" binding:"required"`
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
	}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		log.Fatalf("Error opening Gorm database: %q", err)
	} else {
		db.AutoMigrate(&User{}, &Asset{}, &Account{}, &Order{})
	}
	r := gin.Default()
	r.LoadHTMLGlob("templates/*.tmpl.html")
	r.GET("/", func(c *gin.Context) {
		var assets []Asset
		var users []User
		var orders []Order
		if err := db.Find(&assets).Error; err != nil &&
			!errors.Is(err, gorm.ErrRecordNotFound) {
			c.String(http.StatusInternalServerError, err.Error())
		} else if err := db.Find(&users).Error; err != nil &&
			!errors.Is(err, gorm.ErrRecordNotFound) {
			c.String(http.StatusInternalServerError, err.Error())
		} else if err := db.Preload(clause.Associations).
			Find(&orders).Error; err != nil &&
			!errors.Is(err, gorm.ErrRecordNotFound) {
			c.String(http.StatusInternalServerError, err.Error())
		} else {
			c.HTML(http.StatusOK, "index.tmpl.html", gin.H{
				"assets": assets,
				"users":  users,
				"orders": orders,
			})
		}
	})
	r.GET("/listOrders", func(c *gin.Context) {
		var orders []Order
		if err := db.Preload(clause.Associations).Find(&orders).
			Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			c.String(http.StatusInternalServerError, err.Error())
		} else {
			c.JSON(http.StatusOK, orders)
		}
	})
	r.GET("/listAccount", func(c *gin.Context) {
		var listAccount ListAccount
		if c.Bind(&listAccount) == nil {
			var accounts []Account
			if err := db.Find(&accounts, "user_id", listAccount.UserID).
				Error; err != nil {
				c.String(http.StatusInternalServerError, err.Error())
			} else {
				c.JSON(http.StatusOK, accounts)
			}
		}
	})
	r.POST("/newAsset", func(c *gin.Context) {
		ticker := c.PostForm("ticker")
		if ticker == "" {
			c.String(http.StatusBadRequest, "Ticker is required")
		} else if err := db.Create(Asset{Ticker: ticker}).Error; err != nil {
			c.String(http.StatusBadRequest, err.Error())
		} else {
			c.HTML(http.StatusOK, "newAsset.tmpl.html", gin.H{"ticker": ticker})
		}
	})
	r.POST("/newUser", func(c *gin.Context) {
		user := User{Name: c.PostForm("name")}
		if err := db.Create(&user).Error; err != nil {
			c.String(http.StatusBadRequest, err.Error())
		} else {
			c.JSON(http.StatusOK, user)
		}
	})
	r.POST("/deposit", func(c *gin.Context) {
		var dep Ticket
		if c.Bind(&dep) == nil {
			var account Account
			if err := db.Transaction(deposit(dep, &account)); err != nil {
				c.String(http.StatusInternalServerError, err.Error())
			} else {
				c.JSON(http.StatusOK, account)
			}
		}
	})
	r.POST("/withdraw", func(c *gin.Context) {
		var wit Ticket
		if c.Bind(&wit) == nil {
			var account Account
			if err := db.Transaction(withdraw(wit, &account)); err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					c.String(http.StatusNotFound, err.Error())
				} else if errors.Is(err, errInsufficientFund) {
					c.String(http.StatusPaymentRequired, err.Error())
				} else {
					c.String(http.StatusInternalServerError, err.Error())
				}
			} else {
				c.JSON(http.StatusOK, account)
			}
		}
	})
	r.POST("/makeOrder", func(c *gin.Context) {
		var maker Maker
		if c.Bind(&maker) == nil {
			if err := db.Transaction(func(tx *gorm.DB) error {
				var seller Account
				if err := withdraw(Ticket{
					UserID: maker.UserID,
					Ticker: maker.SellTicker,
					Amount: maker.SellAmount,
				}, &seller)(tx); err != nil {
					return err
				} else {
					buyer := Account{
						UserID:      maker.UserID,
						AssetTicker: maker.BuyTicker,
					}
					if err := tx.Take(&buyer, buyer).
						Error; errors.Is(err, gorm.ErrRecordNotFound) {
						if err := tx.Create(&buyer).Error; err != nil {
							return err
						}
					} else if err != nil {
						return err
					}
					if err := debit(&seller, maker.SellAmount, // bug here
						tx); err != nil { // duplicated debit and withdraw
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
					c.String(http.StatusNotFound, err.Error())
				} else if errors.Is(err, errInsufficientFund) {
					c.String(http.StatusPaymentRequired, err.Error())
				} else {
					c.String(http.StatusInternalServerError, err.Error())
				}
			}
		}
	})
	r.POST("/takeOrder", func(c *gin.Context) {
		var taker Taker
		if c.Bind(&taker) == nil {
			var sellTo Account
			if err := db.Transaction(func(tx *gorm.DB) error {
				var order Order
				if err := db.Preload(clause.Associations).
					Take(&order, taker.OrderID).Error; err != nil {
					return err
				} else {
					var buyFrom Account
					if err := withdraw(Ticket{
						UserID: taker.UserID,
						Ticker: order.Buyer.AssetTicker,
						Amount: order.BuyAmount,
					}, &buyFrom)(tx); err != nil {
						return err
					} else if err := deposit(Ticket{
						UserID: taker.UserID,
						Ticker: order.Seller.AssetTicker,
						Amount: order.SellAmount,
					}, &sellTo)(tx); err != nil {
						return err
					} else if err := credit(&order.Buyer, order.BuyAmount,
						tx); err != nil {
						return err
					} else if err := tx.Delete(&order).Error; err != nil {
						return err
					} else {
						return nil
					}
				}
			}); err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					c.String(http.StatusNotFound, err.Error())
				} else if errors.Is(err, errInsufficientFund) {
					c.String(http.StatusPaymentRequired, err.Error())
				} else {
					c.String(http.StatusInternalServerError, err.Error())
				}
			} else {
				c.JSON(http.StatusOK, sellTo)
			}
		}
	})
	r.Run(":" + port)
}
