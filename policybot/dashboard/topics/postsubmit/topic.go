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

//go:generate ../../../scripts/gen_topic.sh

package postsubmit

import (
	"context"
	"net/http"
	"strings"
	"text/template"
	"os"
	"fmt"
	"strconv"
	"bufio"
	"encoding/csv"
	"istio.io/bots/policybot/dashboard/types"

	"istio.io/bots/policybot/pkg/storage"
	"istio.io/bots/policybot/pkg/storage/cache"
)

// PostSubmit lets users visualize critical information about the project's outstanding pull requests.
type PostSubmit struct {
	store         storage.Store
	cache         *cache.Cache
	latestbaseSha *template.Template
	baseSha       *template.Template
	analysis      *template.Template
}

type LatestBaseShaSummary struct {
	LatestBaseSha []LatestBaseSha
}

type LatestBaseSha struct {
	BaseSha        string
	//LastFinishTime time.Time
	LastFinishTime string
	NumberofTest   int64
}

type BaseShas struct {
	BaseSha        []string
}

type LabelEnvSummary struct {
	LabelEnv []LabelEnv
}

type LabelEnv struct {
	Label  string
	EnvCount    []EnvCount
	SubLabel LabelEnvSummary
}

type EnvCount struct {
	Env      string
	Counts   int
}


// New creates a new PostSubmit instance.
func New(store storage.Store, cache *cache.Cache) *PostSubmit {
	return &PostSubmit{
		store:         store,
		cache:         cache,
		latestbaseSha: template.Must(template.New("page").Parse(string(MustAsset("page.html")))),
		baseSha:       template.Must(template.New("chooseBaseSha").Parse(string(MustAsset("chooseBaseSha.html")))),
		analysis:      template.Must(template.New("analysis").Parse(string(MustAsset("analysis.html")))),
	}
}

func (ps *PostSubmit) RenderLabelEnv(req *http.Request) (types.RenderInfo, error) {
	var summary LabelEnvSummary
	summary, err := ps.getLabelEnvTable(req.Context(),"616aff839f0c4cc71a487f1c2e43b0bc13ce20af")
	if err != nil {
		return types.RenderInfo{}, err
	}
	var sb strings.Builder
	if err := ps.analysis.Execute(&sb, summary); err != nil {
		return types.RenderInfo{}, err
	}
	return types.RenderInfo{
		Content: sb.String(),
	}, nil
}

func (ps *PostSubmit) RenderAllBaseSha(req *http.Request) (types.RenderInfo, error) {
	var chooseBaseSha_page BaseShas
	allBaseShas, err := ps.store.QueryAllBaseSha(req.Context())
	chooseBaseSha_page.BaseSha = allBaseShas
	if err != nil {
		return types.RenderInfo{}, err
	}
	var sb strings.Builder
	if err := ps.baseSha.Execute(&sb, chooseBaseSha_page); err != nil {
		return types.RenderInfo{}, err
	}
	return types.RenderInfo{
		Content: sb.String(),
	}, nil
}

func ReadFromCSV(file string) (summary LatestBaseShaSummary, err error){
	csvfile, err := os.Open(file)
	if err != nil {
		return summary, fmt.Errorf("can't open file: %v", err)
	}
	reader := csv.NewReader(bufio.NewReader(csvfile))

	var baseshaList []LatestBaseSha
    for {
        line, error := reader.Read()
        if error != nil {
           break
		}
		i, _ := strconv.ParseInt(line[2], 10, 64)
		baseshaList= append(baseshaList, LatestBaseSha{
			BaseSha: line[0],
			LastFinishTime: line[1],
			NumberofTest: i,
		})
	}
	summary.LatestBaseSha = baseshaList
	return
}
func (ps *PostSubmit) RenderLatestBaseSha(req *http.Request) (types.RenderInfo, error) {
	/*baseShas, err := ps.getLatestBaseShas(req.Context())
		if err != nil {
		return types.RenderInfo{}, err
	} */
	baseShas, err := ReadFromCSV("basesha.csv")
	if err != nil {
		return types.RenderInfo{}, err
	}
	var sb strings.Builder
	if err := ps.latestbaseSha.Execute(&sb, baseShas); err != nil {
		return types.RenderInfo{}, err
	}

	return types.RenderInfo{
		Content: sb.String(),
	}, nil
}

func (ps *PostSubmit) getLatestBaseShas(context context.Context) (LatestBaseShaSummary, error) {
	var summary LatestBaseShaSummary
	var summaryList []LatestBaseSha

	if err := ps.store.QueryLatestBaseSha(context, func(latestBaseSha *storage.LatestBaseSha) error {
		summaryList = append(summaryList, LatestBaseSha{
			BaseSha: latestBaseSha.BaseSha,
			//LastFinishTime: latestBaseSha.LastFinishTime,
			NumberofTest: latestBaseSha.NumberofTest,
		})
		return nil
	}); err != nil {
		return summary, err
	}
	summary.LatestBaseSha = summaryList
	return summary, nil
}

func (ps *PostSubmit) getLabelEnvTable(context context.Context, baseSha string) (LabelEnvSummary, error) {
	var summary LabelEnvSummary
	var Labels = make(map[string]map[string]int)
	//var LabelTree = make(map[string]map[string]int)

	if err := ps.store.QueryPostSubmitTestResult(context, baseSha, func(postSubmitTestResult *storage.PostSubmitTestResultDenormalized) error {
		_, ok := Labels[postSubmitTestResult.Label]
		if !ok {
			Labels[postSubmitTestResult.Label] = make(map[string]int)
		}
		Labels[postSubmitTestResult.Label][postSubmitTestResult.Environment]++
		return nil
	}); err != nil {
		return summary, err
	}
	return ps.getLabelTree(Labels,0), nil
}

func (ps *PostSubmit) getLabelTree(input map[string]map[string]int, depth int) (LabelEnvSummary){
	if len(input)<1{
		return LabelEnvSummary{}
	}
	if depth > 2 {
		return LabelEnvSummary{}
	}
	var toplayer = make(map[string]map[string]int)
	var nextLayer = make(map[string]map[string]map[string]int)
	var nextLayerSummary = make(map[string]LabelEnvSummary)
	for label, envMap := range input{
		splitlabel := strings.Split(label, ".")
		_, ok := toplayer[splitlabel[0]]
		if !ok {
			toplayer[splitlabel[0]] = make(map[string]int)
		} 
		for env, count := range envMap {
			toplayer[splitlabel[0]][env] += count
		}
		//add content after first dot to the map for the next layer
		if len(splitlabel)<2{
			continue
		}
		_, ok = nextLayer[splitlabel[0]]
		if !ok {
			nextLayer[splitlabel[0]] = make(map[string]map[string]int)
		}
		nextLayer[splitlabel[0]][strings.Join(splitlabel[1:], ".")] = envMap
	}

	for topLayerName, nextLayerMap := range nextLayer{
		nextLayerSummary[topLayerName] = ps.getLabelTree(nextLayerMap, depth+1)
	}
	return ps.convertMapToSummary(toplayer, nextLayerSummary)
}

func (ps *PostSubmit) convertMapToSummary (input map[string]map[string]int, nextLayer map[string]LabelEnvSummary) (summary LabelEnvSummary){
	var labelEnvList []LabelEnv
	for label, envMap := range input {
		var labelEnv LabelEnv 
		var newEnvCount []EnvCount
		for env, count := range envMap {
			newEnvCount = append(newEnvCount, EnvCount{Env:env, Counts:count})
		}
		labelEnv.Label = label
		labelEnv.EnvCount = newEnvCount
		labelEnv.SubLabel = nextLayer[label]
		labelEnvList = append(labelEnvList, labelEnv)
	}
	summary.LabelEnv = labelEnvList
	return
}