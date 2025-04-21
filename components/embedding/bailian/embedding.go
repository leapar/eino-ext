/*
3dman.cn

百炼多模态向量 https://bailian.console.aliyun.com/?tab=api#/api/?type=model&url=https%3A%2F%2Fhelp.aliyun.com%2Fdocument_detail%2F2712517.html

其实还可以通过gocv  resnet来提取图片向量

```go

// Read the network using the ResNet50 ONNX model
net := gocv.ReadNetFromONNX("resnet50-v1-7.onnx")
// Set preferable backend and target to CPU
err := net.SetPreferableBackend(gocv.NetBackendDefault)
net.SetPreferableTarget(gocv.NetTargetCPU)

embeddingMat := net.Forward("resnetv17_dense0_fwd")

// Extract the data as a float32 slice
embedding, err := embeddingMat.DataPtrFloat32()

```
// 如果为了图片向量准确度还可以调用云图片识别的api，把图片识别出来的label标签也加入到向量中
// 例如提取到了桌子、凳子、瓷器
// 向量后面再拼接3个1.0浮点数
*/

package bailian

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cloudwego/eino/components/embedding"
)

const (
	baseUrl        = "https://dashscope.aliyuncs.com/api/v1/services/embeddings/multimodal-embedding/multimodal-embedding"
	dimensions     = 1024
	defaultTimeout = 10 * time.Minute
)

type EmbeddingConfig struct {
	// APIKey 百炼大平台的api key
	APIKey string `json:"api_key"`
	// Timeout specifies the http request timeout.
	// If HTTPClient is set, Timeout will not be used.
	Timeout time.Duration `json:"timeout"`
	// HTTPClient specifies the client to send HTTP requests.
	// If HTTPClient is set, Timeout will not be used.
	// Optional. Default &http.Client{Timeout: Timeout}
	HTTPClient *http.Client `json:"http_client"`

	// Model available models: multimodal-embedding-v1
	Model string `json:"model"`
}

type Embedder struct {
	config *EmbeddingConfig
}

func NewEmbedder(ctx context.Context, config *EmbeddingConfig) (*Embedder, error) {
	if len(config.Model) == 0 {
		config.Model = "multimodal-embedding-v1"
	}

	if config.Timeout == 0 {
		config.Timeout = defaultTimeout
	}

	if len(config.APIKey) == 0 {
		return nil, fmt.Errorf("invalid base api key")
	}

	var httpClient *http.Client

	if config.HTTPClient != nil {
		httpClient = config.HTTPClient
	} else {
		httpClient = &http.Client{Timeout: config.Timeout}
	}

	config.HTTPClient = httpClient

	return &Embedder{config: config}, nil
}

/*
{"text": "通用多模态表征模型"},
{"image": "https://mitalinlp.oss-cn-hangzhou.aliyuncs.com/dingkun/images/1712648554702.jpg"},
{"video": "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20250107/lbcemt/new+video.mp4"}
*/
func (e *Embedder) EmbedMultiModal(ctx context.Context, texts []map[string]string, opts ...embedding.Option) ([][]float64, error) {

	config := &RequestConfig{
		Model:  e.config.Model,
		ApiKey: e.config.APIKey,
		Input: &RequestConfigInput{
			Contents: make([]map[string]string, 0),
		},
		Parameters: &RequestConfigParams{},
	}

	config.Input.Contents = append(config.Input.Contents, texts...)

	reposeData, err := doEmbeddings(e.config.HTTPClient, config)
	if err != nil || reposeData.Output == nil {
		return nil, err
	}

	embeddings := make([][]float64, len(reposeData.Output.Embeddings))

	for _, embedding := range reposeData.Output.Embeddings {
		res := embedding.Embedding[:]
		embeddings[embedding.Index] = res
	}

	return embeddings, nil

}

const typ = "BaiLian"

func (e *Embedder) GetType() string {
	return typ
}

func (e *Embedder) IsCallbacksEnabled() bool {
	return true
}

// 请求参数
type RequestConfigInput struct {
	Contents []map[string]string `json:"contents"`
}

type RequestConfigParams struct {
}

type RequestConfig struct {
	Model      string               `json:"model"`
	ApiKey     string               `json:"-"`
	Input      *RequestConfigInput  `json:"input"`
	Parameters *RequestConfigParams `json:"parameters"`
}

// 接口返回数据
type ReposeDataUsage struct {
	InputTokens int     `json:"input_tokens"`
	ImageCount  int     `json:"image_count"`
	Duration    float64 `json:"duration"`
}

type ReposeDataOutputEmbeddings struct {
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
	Type      string    `json:"type"`
}

type ReposeDataOutput struct {
	Embeddings []*ReposeDataOutputEmbeddings `json:"embeddings"`
}

type ReposeData struct {
	Output    *ReposeDataOutput `json:"output"`
	Usage     *ReposeDataUsage  `json:"usage"`
	RequestId string            `json:"request_id"`
}

func doEmbeddings(httpClient *http.Client, config *RequestConfig) (*ReposeData, error) {
	param, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	var jsonData io.Reader
	//把[]byte 转成实现了read接口的Reader结构体
	if param != nil {
		jsonData = bytes.NewReader(param)
	}
	req, err := http.NewRequest("POST", baseUrl, jsonData)
	if err != nil {
		err = fmt.Errorf("网络故障")
		return nil, err
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", config.ApiKey))

	resp, err := httpClient.Do(req)
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
