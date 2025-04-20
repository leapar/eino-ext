package bailian

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/cloudwego/eino/schema"
)

const (
	ApiURL = "https://dashscope.aliyuncs.com/api/v1/services/rerank/text-rerank/text-rerank"
)

type ReRanker struct {
	config *ReRankerConfig
}

type ReRankerConfig struct {
	Model           string //模型
	ReturnDocuments bool   //是否返回documents
	TopK            int    //top_n
	ScoreThreshold  float64
	ApiKey          string //平台ApiKey
}

func NewReRanker(ctx context.Context, opt *ReRankerConfig) (*ReRanker, error) {
	config := &ReRankerConfig{
		Model:           "gte-rerank",
		ReturnDocuments: false,
		TopK:            5,
		ScoreThreshold:  0.7,
	}
	if opt != nil {
		if opt.TopK > 0 {
			config.TopK = opt.TopK
		}
		config.ScoreThreshold = opt.ScoreThreshold
		config.ReturnDocuments = opt.ReturnDocuments
		config.Model = opt.Model
		config.ApiKey = opt.ApiKey
	}
	reRanker := &ReRanker{config: config}
	return reRanker, nil
}

func (impl *ReRanker) ReRankDocuments(ctx context.Context, src []*schema.Document, query string) ([]*schema.Document, error) {
	//小于两条不排序
	if len(src) <= 1 {
		return src, nil
	}
	config := &RequestConfig{
		Model:  impl.config.Model,
		ApiKey: impl.config.ApiKey,
		Input: &RequestConfigInput{
			Query:     query,
			Documents: make([]string, 0),
		},
		Parameters: &RequestConfigParams{
			ReturnDocuments: impl.config.ReturnDocuments,
			TopK:            impl.config.TopK,
		},
	}
	for _, v := range src {
		config.Input.Documents = append(config.Input.Documents, v.Content)
	}
	reRankData, err := doAliRerank(config)
	if err != nil {
		return src, nil
	}
	dst := make([]*schema.Document, 0)
	for i := 0; i < len(reRankData.Output.Results); i++ {
		res := reRankData.Output.Results[i]
		src[res.Index].WithScore(res.Score)
		dst = append(dst, src[res.Index])
	}

	return dst, nil
}

// 请求参数
type RequestConfigInput struct {
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
}

type RequestConfigParams struct {
	ReturnDocuments bool `json:"return_documents"`
	TopK            int  `json:"top_n"`
}

type RequestConfig struct {
	Model      string               `json:"model"`
	ApiKey     string               `json:"-"`
	Input      *RequestConfigInput  `json:"input"`
	Parameters *RequestConfigParams `json:"parameters"`
}

// 接口返回数据
type ReposeDataUsage struct {
	TotalTokens int `json:"total_tokens"`
}

type ReposeDataOutputResult struct {
	Index int     `json:"index"`
	Score float64 `json:"relevance_score"`
}

type ReposeDataOutput struct {
	Results []*ReposeDataOutputResult `json:"results"`
}

type ReposeData struct {
	Output    *ReposeDataOutput `json:"output"`
	Usage     *ReposeDataUsage  `json:"usage"`
	RequestId string            `json:"request_id"`
}

func doAliRerank(config *RequestConfig) (*ReposeData, error) {
	param, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	var jsonData io.Reader
	//把[]byte 转成实现了read接口的Reader结构体
	if param != nil {
		jsonData = bytes.NewReader(param)
	}
	req, err := http.NewRequest("POST", ApiURL, jsonData)
	if err != nil {
		err = fmt.Errorf("网络故障")
		return nil, err
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", config.ApiKey))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	resp_body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	reRankData := &ReposeData{}
	err = json.Unmarshal(resp_body, reRankData)
	if err != nil {
		fmt.Println(string(resp_body))
	}
	if reRankData.Output == nil {
		return nil, fmt.Errorf("no data")
	}
	return reRankData, err
}
