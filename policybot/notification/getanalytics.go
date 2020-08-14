// Copyright 2019 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package notification

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"google.golang.org/api/analytics/v3"

	"istio.io/bots/policybot/pkg/cmdutil"
	"istio.io/bots/policybot/pkg/config"
)

var gaTableID = "ga:149552698"

func findEmailToWhom(addr string, docsOwnerMap map[string]string) string {
	for page, owner := range docsOwnerMap {
		if strings.Contains(addr, page) {
			return owner
		}
	}
	return "istio/wg-docs-maintainers"
}

func findEmailToWhomMap(ownersMap map[string]map[string]string, addrs []string,
	event string) (map[string]map[string]string, error) {
	docsOwnerMap, err := ReadDocsOwner()
	if err != nil {
		return nil, fmt.Errorf("unable to read docs owner %v", err)
	}

	for _, addr := range addrs {
		owner := findEmailToWhom(addr, docsOwnerMap)
		_, ok := ownersMap[owner]
		if !ok {
			ownersMap[owner] = make(map[string]string)
		}
		ownersMap[owner][event] += addr + "<br>"
	}
	return ownersMap, nil
}

func getData(dataGaGetCall *analytics.DataGaGetCall, dimensions string, filter string, sort string) (*analytics.GaData, error) {
	//set up dimension
	if dimensions != "" {
		dataGaGetCall.Dimensions(dimensions)
	}
	// setup the filter
	if filter != "" {
		dataGaGetCall.Filters(filter)
	}
	//set up sort
	if sort != "" {
		dataGaGetCall.Sort(sort)
	}
	getData, err := dataGaGetCall.Do()
	if err != nil {
		return nil, err
	}
	return getData, nil
}

func getInternal404Error(dataService *analytics.DataGaService, startdate string, enddate string) (*analytics.GaData, error) {
	metric := "ga:uniquePageviews"
	dataGaGetCall := dataService.Get(gaTableID, startdate, enddate, metric)
	filter := "ga:previousPagePath!=(entrance);ga:pageTitle=~404"
	dimensions := "ga:pagePath"
	sort := "-ga:uniquePageviews"
	return getData(dataGaGetCall, dimensions, filter, sort)
}

func getExternal404Error(dataService *analytics.DataGaService, startdate string, enddate string) (*analytics.GaData, error) {
	metric := "ga:uniquePageviews"
	dataGaGetCall := dataService.Get(gaTableID, startdate, enddate, metric)
	filter := "ga:previousPagePath==(entrance);ga:pageTitle=~404"
	dimensions := "ga:pagePath"
	sort := "-ga:uniquePageviews"
	return getData(dataGaGetCall, dimensions, filter, sort)
}

func getRepeatedBadReviews(dataService *analytics.DataGaService, startdate string, enddate string) (*analytics.GaData, error) {
	metrics := "ga:totalEvents,ga:eventValue"
	dataGaGetCall := dataService.Get(gaTableID, startdate, enddate, metrics)
	dimensions := "ga:pagePath"
	filter := "ga:totalEvents>10"
	return getData(dataGaGetCall, dimensions, filter, "")
}

func getPageview(dataService *analytics.DataGaService, startdate string, enddate string) (*analytics.GaData, error) {
	metrics := "ga:pageviews"
	dataGaGetCall := dataService.Get(gaTableID, startdate, enddate, metrics)
	dimensions := "ga:pageTitle"
	filter := "ga:pageviews>10"
	return getData(dataGaGetCall, dimensions, filter, "")
}
func getLastweekPageview(dataService *analytics.DataGaService, filter string) (string, error) {
	metrics := "ga:pageviews"
	startdate := time.Now().Add(time.Hour * 24 * -14).Format("2006-01-02")
	enddate := time.Now().Add(time.Hour * 24 * -7).Format("2006-01-02")
	dataGaGetCall := dataService.Get(gaTableID, startdate, enddate, metrics)
	getData, err := getData(dataGaGetCall, "", filter, "")
	if err != nil {
		return "", fmt.Errorf("unable to get lastweek's pageview: %v", err)
	}
	return getData.Rows[0][0], nil
}

func getDataService() (dataService *analytics.DataGaService, err error) {
	ctx := context.Background()
	analyticService, err := analytics.NewService(ctx)

	if err != nil {
		return nil, fmt.Errorf("error creating analytics service: %v", err)
	}

	dataService = analytics.NewDataGaService(analyticService)
	return dataService, nil
}

//get links to the 404 error page
func getLink(getData *analytics.GaData) (linkList []string) {
	for row := 0; row <= len(getData.Rows)-1; row++ {
		linkList = append(linkList, getData.Rows[row][0])
	}
	return
}

func getBadReviewMessage(getData *analytics.GaData) (message string, err error) {
	for row := 0; row <= len(getData.Rows)-1; row++ {
		totalevent, err := strconv.Atoi(getData.Rows[row][1])
		if err != nil {
			return "", fmt.Errorf("can't convert total events to number: %v", err)
		}
		eventvalue, err := strconv.Atoi(getData.Rows[row][2])
		if err != nil {
			return "", fmt.Errorf("can't convert event value to number: %v", err)
		}
		if eventvalue*2 < totalevent {
			message += "Link: " + getData.Rows[row][0] + " number of bad review last week: " +
				strconv.Itoa(totalevent-eventvalue) + "<br>"
		}
	}
	return
}

func getPageviewChanges(dataService *analytics.DataGaService, getData *analytics.GaData) (message string, err error) {
	for row := 0; row <= len(getData.Rows)-1; row++ {
		link := getData.Rows[row][0]
		pageview, err := strconv.Atoi(getData.Rows[row][1])
		if err != nil {
			return "", fmt.Errorf("can't convert pageview to number: %v", err)
		}
		filter := "ga:pageTitle==" + link
		lastweek, err := getLastweekPageview(dataService, filter)
		if err != nil {
			return "", fmt.Errorf("can't get last week's pageview from analytics: %v", err)
		}
		lastweekPageview, err := strconv.Atoi(lastweek)
		if err != nil {
			return "", fmt.Errorf("can't convert last week pageview to number: %v", err)
		}
		if float64(pageview) > 1.3*float64(lastweekPageview) {
			message += link + ": increase 30% pageview compared to last week"
		} else if float64(pageview) < 0.7*float64(lastweekPageview) {
			message += link + ": decrease 30% pageview compared to last week"
		}
	}
	return
}
func DailyReport(reg *config.Registry, secrets *cmdutil.Secrets) error {
	var (
		enddate   string = time.Now().Format("2006-01-02")
		startdate string = time.Now().Add(time.Hour * 24 * -1).Format("2006-01-02")
	)
	dataService, err := getDataService()
	if err != nil {
		return err
	}
	internal404Message, err := getInternal404Error(dataService, startdate, enddate)
	if err != nil {
		return fmt.Errorf("unable to get internal 404 error page from analytics: %v", err)
	}
	external404Message, err := getExternal404Error(dataService, startdate, enddate)
	if err != nil {
		return fmt.Errorf("unable tot external 404 error page from analytics: %v", err)
	}

	ownersMap := make(map[string]map[string]string)
	ownersMap, err = findEmailToWhomMap(ownersMap, getLink(internal404Message), "internal404error")
	if err != nil {
		return err
	}
	ownersMap, err = findEmailToWhomMap(ownersMap, getLink(external404Message), "external404error")
	if err != nil {
		return err
	}

	for owner, eventsMap := range ownersMap {
		message := ""
		for event, link := range eventsMap {
			message += event + link
		}
		err := SendGithubIssueComment(secrets, "@"+owner+" "+message)
		if err != nil {
			return fmt.Errorf("unable to comment under Github issue: %v", err)
		}
	}
	return nil
}

func WeeklyReport(reg *config.Registry, secrets *cmdutil.Secrets) error {
	var (
		enddate   string = time.Now().Format("2006-01-02")
		startdate string = time.Now().Add(time.Hour * 24 * -7).Format("2006-01-02")
	)

	dataService, err := getDataService()
	if err != nil {
		return err
	}

	eventsData, err := getRepeatedBadReviews(dataService, startdate, enddate)
	if err != nil {
		return fmt.Errorf("unable to get bad reviews data from analytics: %v", err)
	}
	badReviewMessage, err := getBadReviewMessage(eventsData)
	if err != nil {
		return err
	}
	if badReviewMessage != "" {
		err = SendGithubIssueComment(secrets, "@istio/wg-docs-maintainers "+
			"the following pages get many bad reviews in last week"+badReviewMessage)
		if err != nil {
			return fmt.Errorf("unable to comment under Github issue: %v", err)
		}
	}

	pageview, err := getPageview(dataService, startdate, enddate)
	if err != nil {
		return fmt.Errorf("unable to get pageview from analytics: %v", err)
	}
	pageviewChangesMessage, err := getPageviewChanges(dataService, pageview)
	if err != nil {
		return err
	}
	if pageviewChangesMessage != "" {
		err = SendGithubIssueComment(secrets, "@istio/wg-docs-maintainers "+
			"the following pages pageview increase/decrease more than 30% compared to last week"+badReviewMessage)
		if err != nil {
			return fmt.Errorf("unable to comment under Github issue: %v", err)
		}
	}
	return nil
}
