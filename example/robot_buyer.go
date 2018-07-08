package main

import (
	"fmt"
	"github.com/go-kit/kit/log/level"
	"database/sql"
	"time"
	"math"
)

/*
 * Robot Project Manager
 */
func ProjectManager(){

	// Project Performance Indicator Refreshing
	ProjectMutex.Lock()
	for _, project := range AliveProjectList {

		netBuy,netIncome := GetProjectNetBuy(project.id)

		project.BalanceBase = netBuy
		project.BalanceQuote = netIncome + project.InitialBalance

		// get latest price
		highestBid := OBData{}
		highestBid = getHighestBid(project.Symbol)
		if highestBid.Time.Add(time.Second * 60).Before(time.Now()) {
			fmt.Println("Warning! ProjectManager - getHighestBid got old data. fail to update Roi for", project.Symbol)
			continue
		}

		project.Roi = (project.BalanceQuote + project.BalanceBase * highestBid.Price) / project.InitialBalance - 1.0
		fmt.Printf("ProjectManager - %s: Roi=%.2f%%, RoiS=%.2f%%\n",
			project.Symbol, project.Roi*100, project.RoiS*100)

		// Update Roi to Database
		if !UpdateProjectRoi(&project){
			fmt.Println("ProjectManager - Warning! UpdateProjectRoi fail.")
		}


	}
	ProjectMutex.Unlock()

}

/*
	Quit Conditions:
	1、TotalLoss > 20%
	2、TotalGain > 40%
	3、Loss in latest 1 hour  				（N1HourPrice, N1HourRoi)
	4、Loss or Gain < 5% in latest 3 hours   (N3HourPrice, N1HourRoi)
	5、Loss or Gain < 5% in latest 6 hours 	 (N6HourPrice, N6HourRoi)
	6、Over 12 Hours project
	7、Manual Command：ForceQuit = True, Amount: 25%、50%、75%、100%、Default(100%)

	Others:
	1、For（1、3、4、5），add into BlackList for 2 hours
	2、QuitProtect=True，no automatic quite；only ForceQuit = True can make project quit.
 */
func QuitDecisionMake(project *ProjectData) (bool,bool){

	if project.ForceQuit {
		return true,false	//quit w/o blacklist
	}

	if project.QuitProtect {
		return false,false
	}

	if project.Roi <= -0.2{
		return true,true	//quit w/ blacklist
	}

	if project.Roi >= 0.4 {
		return true,false	//quit w/o blacklist
	}

	if project.CreateTime.Add(time.Hour * 12).Before(time.Now()) {
		return true,false	//quit w/o blacklist
	}

	roiData := GetLatestRoi(project.Symbol, 1.0)
	if roiData!=nil && roiData.RoiD < 0{
		return true,true	//quit w/ blacklist
	}

	roiData = GetLatestRoi(project.Symbol, 3.0)
	if roiData!=nil && roiData.RoiD < 0.05{
		return true,true	//quit w/ blacklist
	}

	roiData = GetLatestRoi(project.Symbol, 6.0)
	if roiData!=nil && roiData.RoiD < 0.05{
		return true,true	//quit w/ blacklist
	}

	return false,false
}

/*
 * Back Window: for example 1Hour, 3Hour, 6Hour
 */
func GetLatestRoi(symbol string, backTimeWindow float64) *RoiData{

	if backTimeWindow < 0.5 || backTimeWindow>120 {
		fmt.Println("GetLatestRoi - Error! backTimeWindow out of range [0.5,120].", backTimeWindow)
		return nil
	}

	i := 0
	var s string
	for i, s = range SymbolList {
		if symbol == s {
			break
		}
	}
	if SymbolList[i] != symbol {
		fmt.Println("GetLatestRoi - Fail to find symbol in SymbolList", symbol)
		return nil
	}

	klinesMap := SymbolKlinesMapList[i]

	var nowOpenTime= time.Time{}
	var nowCloseTime= time.Time{}
	var nowClose float64 = 0.0

	// find the latest OpenTime
	// TODO: just use Now() to get neareast 5 minutes
	for _, v := range klinesMap {
		if v.OpenTime.After(nowOpenTime) {
			nowOpenTime = v.OpenTime
			nowCloseTime = v.CloseTime
			nowClose = v.Close
		}
	}

	N := int(math.Round(backTimeWindow * 60 / 5))

	roiData := CalcRoi(symbol,
		N,
		nowOpenTime,
		nowCloseTime,
		nowClose,
		klinesMap)

	return &roiData
}

/*
 * Get Order Net Buy (total Buy - total Sell) for ProjectID, based on database records.
 *	  and Net Income (total Income - total Spent).
 */
func GetProjectNetBuy(projectId int64) (float64,float64){

	// Sum all the Buy

	rowBuy := DBCon.QueryRow(
		"select sum(ExecutedQty),sum(ExecutedQty*Price) from order_list " +
		"where Side='BUY' and Status='FILLED' and IsDone=1 and ProjectID=?;",
		projectId)

	totalExecutedBuyQty := 0.0
	totalSpentQuote := 0.0
	errB := rowBuy.Scan(&totalExecutedBuyQty, &totalSpentQuote)

	if errB != nil && errB != sql.ErrNoRows {
		level.Error(logger).Log("GetProjectSum - DB.Query Fail. Err=", errB)
		panic(errB.Error())
	}

	// Sum all the Sell

	rowSell := DBCon.QueryRow(
		"select sum(ExecutedQty),sum(ExecutedQty*Price) from order_list " +
		"where Side='SELL' and Status='FILLED' and IsDone=1 and ProjectID=?;",
		projectId)

	totalExecutedSellQty := 0.0
	totalIncomeQuote := 0.0

	var t1,t2 NullFloat64
	errS := rowSell.Scan(&t1, &t2)

	if errS != nil && errS != sql.ErrNoRows {
		level.Error(logger).Log("GetProjectSum - DB.Query Fail. Err=", errS)
		panic(errS.Error())
	}

	if t1.Valid {
		totalExecutedSellQty = t1.Float64
	}
	if t2.Valid {
		totalIncomeQuote = t2.Float64
	}

	//fmt.Println("GetProjectSum - TotalBuyQty:", totalExecutedBuyQty, "TotalSellQty:", totalExecutedSellQty,
	//	". TotalSpent:", totalSpentQuote, "TotalIncome:", totalIncomeQuote,
	//	"ProjectId =", projectId)
	return totalExecutedBuyQty-totalExecutedSellQty, totalIncomeQuote-totalSpentQuote
}

/*
 * Update Project Roi into Database
 */
func UpdateProjectRoi(project *ProjectData) bool{

	if project==nil || len(project.ClientOrderID)==0 || project.OrderID==0 || project.id<0 {
		level.Warn(logger).Log("UpdateProjectRoi.ProjectData", project)
		return false
	}

	query := `UPDATE project_list SET Roi=?, BalanceBase=?, BalanceQuote=? WHERE id=?`

	res, err := DBCon.Exec(query,
		project.Roi,
		project.BalanceBase,
		project.BalanceQuote,
		project.id,
	)

	if err != nil {
		level.Error(logger).Log("DBCon.Exec", err)
		return false
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected>=0 {
		return true
	}else{
		return false
	}
}
