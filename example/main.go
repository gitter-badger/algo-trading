package main

import (
	"context"
	"fmt"
	"os"
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/garyyu/go-binance"
	"os/signal"
	"time"
)

var (
	DBCon 				*sql.DB				// the connection handle for the database
	binanceSrv 			binance.Binance
	routinesExitChan	chan bool
	logger 				log.Logger
)

func main() {

	logger = log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	logger = level.NewFilter(logger, level.AllowAll())
	logger = log.With(logger, "time", log.DefaultTimestampUTC, "caller", log.DefaultCaller)

	var err error
	DBCon, err = sql.Open("mysql",
		os.Getenv("BINANCE_DB_USER")+":"+
		os.Getenv("BINANCE_DB_PWD")+
		"@/binance?parseTime=true")
	if err != nil {
		panic(err.Error())
	}
	defer DBCon.Close()

	hmacSigner := &binance.HmacSigner{
		Key: []byte(os.Getenv("BINANCE_SECRET")),
	}
	ctx, cancelCtx := context.WithCancel(context.Background())
	// use second return value for cancelling request
	binanceService := binance.NewAPIService(
		"https://www.binance.com",
		os.Getenv("BINANCE_APIKEY"),
		hmacSigner,
		logger,
		ctx,
	)
	binanceSrv = binance.NewBinance(binanceService)

	routinesExitChan = make(chan bool)
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	go updateOhlc()

	//go RoiRoutine()

	fmt.Println("main is runing and waiting for interrupt")
	<-interrupt
	fmt.Println("Interrupt received. Canceling context ...")

	// notify all routines exit.
	close(routinesExitChan)
	time.Sleep(1 * time.Second)		// wait 1 seconds for routines exit

	cancelCtx()
	fmt.Println("waiting for signal")

	fmt.Println("main exited.")
	return
}
