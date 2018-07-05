package main

import (
	"github.com/garyyu/go-binance"
	"github.com/go-kit/kit/log/level"
	"time"
	"fmt"
	"strconv"
	"strings"
)

type ObType int

const (
	Bid ObType = iota
	Ask
	Na
)
type OBData struct {
	id						 int		`json:"id"`
	LastUpdateID			 int		`json:"LastUpdateID"`
	Symbol            		 string 	`json:"Symbol"`
	Type 					 ObType		`json:"Type"`
	Price					 float64	`json:"Price"`
	Quantity				 float64	`json:"Quantity"`
	Time                	 time.Time	`json:"Time"`
}

/*
 *  Main Routine for OrderBook
 */
func OrderBookRoutine(){

	time.Sleep(15 * time.Second)

	fmt.Printf("\nOrdBkTick Start: \t%s\n\n", time.Now().Format("2006-01-02 15:04:05.004005683"))

	// start a goroutine to get realtime ROI analysis in 1 min interval
	ticker := robotSecondTicker()
	var tickerCount = 0
loop:
	for  {
		select {
		case _ = <- routinesExitChan:
			break loop
		case tick := <-ticker.C:
			ticker.Stop()

			tickerCount += 1
			logStr := fmt.Sprintf("OrdBkTick: \t\t%s\t%d", tick.Format("2006-01-02 15:04:05.004005683"), tickerCount)
			//hour, min, sec := tick.Clock()

			for _, symbol := range SymbolList {
				getOrderBook(symbol, 10)
			}

			highestBid := getHighestBid("KEYBTC")
			fmt.Printf("%s \tHighest Bid: id=%d, price=%.8f\n", logStr, highestBid.id, highestBid.Price)

			// Update the ticker
			ticker = robotSecondTicker()

		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	fmt.Println("goroutine exited - OrderBookRoutine")
}

func getOrderBook(symbol string, limit int) (int,int){

	var bidsNum = 0
	var asksNum = 0
	var retry = 0
	for {
		retry += 1

		ob, err := binanceSrv.OrderBook(binance.OrderBookRequest{
			Symbol:   symbol,
			Limit:    limit,
		})
		if err != nil {
			level.Error(logger).Log("getOrderBook.Symbol", symbol, "Err", err, "Retry", retry-1)
			if retry >= 10 {
				break
			}

			switch retry {
			case 1:
				time.Sleep(1 * time.Second)
			case 2:
				time.Sleep(1 * time.Second)
			case 3:
				time.Sleep(3 * time.Second)
			case 4:
				time.Sleep(3 * time.Second)
			default:
				time.Sleep(5 * time.Second)
			}
			continue
		}

		if getLastUpdateId(symbol, ob.LastUpdateID) < 0 {
			saveOrderBook(symbol, "binance.com", ob)
			bidsNum = len(ob.Bids)
			asksNum = len(ob.Asks)
		}else{
			fmt.Println("getOrderBook: got same LastUpdateID - ", ob.LastUpdateID)
		}

		break
	}

	return bidsNum,asksNum
}

func saveOrderBook(symbol string, exchangeName string, ob *binance.OrderBook) error {

	if exchangeName != "binance.com"{
		level.Error(logger).Log("saveOrderBook.TODO", exchangeName)
		return nil
	}

	if len(ob.Asks)==0 && len(ob.Bids)==0 {
		level.Error(logger).Log("saveOrderBook.Empty", ob)
		return nil
	}

	sqlStr := "INSERT INTO ob_binance (LastUpdateID, Symbol, Type, Price, Quantity, Time) VALUES "
	var vals []interface{}

	// Bids
	for i:=len(ob.Bids)-1; i>=0; i-- {
		row := ob.Bids[i]
		sqlStr += "(?, ?, ?, ?, ?, NOW()),"
		vals = append(vals, ob.LastUpdateID, symbol, "Bid", row.Price, row.Quantity)
	}
	// Asks
	for _, row := range ob.Asks {
		sqlStr += "(?, ?, ?, ?, ?, NOW()),"
		vals = append(vals, ob.LastUpdateID, symbol, "Ask", row.Price, row.Quantity)
	}
	//trim the last ,
	sqlStr = strings.TrimSuffix(sqlStr, ",")

	stmt, err := DBCon.Prepare(sqlStr)
	if err != nil {
		level.Error(logger).Log("DBCon.Prepare", err)
		return err
	}
	_, err2 := stmt.Exec(vals...)
	if err2 != nil {
		level.Error(logger).Log("DBCon.Exec", err2)
		return err2
	}

	//id, _ := res.LastInsertId()
	return nil
}


func getLastUpdateId(symbol string, LastUpdateID int) int{

	rows, err := DBCon.Query("select id from ob_binance where Symbol='" +
		symbol + "' and LastUpdateID='" + strconv.Itoa(LastUpdateID) + "' limit 1")

	if err != nil {
		panic(err.Error()) // proper error handling instead of panic in your app
	}
	defer rows.Close()

	var id int = -1	// if not found, rows is empty.
	for rows.Next() {
		err := rows.Scan(&id)
		if err != nil {
			level.Error(logger).Log("getLastUpdateId.err", err)
			id = -1
		}
	}
	return id
}


func getHighestBid(symbol string) OBData{

	rows, err := DBCon.Query("select * from ob_binance where Symbol='" +
		symbol + "' and Type='Bid' order by id desc limit 1")

	if err != nil {
		panic(err.Error()) // proper error handling instead of panic in your app
	}
	defer rows.Close()

	obData := OBData{id: -1}
	var obType string
	for rows.Next() {
		err := rows.Scan(&obData.id, &obData.LastUpdateID, &obData.Symbol, &obType,
			&obData.Price, &obData.Quantity, &obData.Time)
		if err != nil {
			level.Error(logger).Log("getHighestBid.err", err)
		}

		switch obType {
		case "Ask":	obData.Type = Ask
		case "Bid": obData.Type = Bid
		default: 	obData.Type = Na
		}
		break
	}
	return obData
}

func robotSecondTicker() *time.Ticker {
	// Return new ticker that triggers on the second
	now := time.Now()
	return time.NewTicker(
		time.Second * time.Duration(3) -
			time.Nanosecond * time.Duration(now.Nanosecond()))
}
