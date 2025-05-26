/*
 * Copyright 2025 CloudWeGo Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package milvus

import (
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

const (
	ConsistencyLevelStrong     ConsistencyLevel = 1
	ConsistencyLevelSession    ConsistencyLevel = 2
	ConsistencyLevelBounded    ConsistencyLevel = 3
	ConsistencyLevelEventually ConsistencyLevel = 4
	ConsistencyLevelCustomized ConsistencyLevel = 5
	
	L2             = MetricType(entity.L2)
	IP             = MetricType(entity.IP)
	CONSINE        = MetricType(entity.COSINE)
	HAMMING        = MetricType(entity.HAMMING)
	JACCARD        = MetricType(entity.JACCARD)
	TANIMOTO       = MetricType(entity.TANIMOTO)
	SUBSTRUCTURE   = MetricType(entity.SUBSTRUCTURE)
	SUPERSTRUCTURE = MetricType(entity.SUPERSTRUCTURE)
)

// defaultSchema is the default schema for milvus by eino
type defaultSchema struct {
	ID       string `json:"id" milvus:"name:id"`
	Content  string `json:"content" milvus:"name:content"`
	Vector   []byte `json:"vector" milvus:"name:vector"`
	Metadata []byte `json:"metadata" milvus:"name:metadata"`
}

func getDefaultFields() []*entity.Field {
	return []*entity.Field{
		entity.NewField().
			WithName(defaultCollectionID).
			WithDescription(defaultCollectionIDDesc).
			WithIsPrimaryKey(true).
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(255),
		entity.NewField().
			WithName(defaultCollectionVector).
			WithDescription(defaultCollectionVectorDesc).
			WithIsPrimaryKey(false).
			WithDataType(entity.FieldTypeBinaryVector).
			WithDim(defaultDim),
		entity.NewField().
			WithName(defaultCollectionContent).
			WithDescription(defaultCollectionContentDesc).
			WithIsPrimaryKey(false).
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(1024),
		entity.NewField().
			WithName(defaultCollectionMetadata).
			WithDescription(defaultCollectionMetadataDesc).
			WithIsPrimaryKey(false).
			WithDataType(entity.FieldTypeJSON),
	}
}

type ConsistencyLevel entity.ConsistencyLevel

func (c *ConsistencyLevel) getConsistencyLevel() entity.ConsistencyLevel {
	return entity.ConsistencyLevel(*c - 1)
}

// MetricType is the metric type for vector by eino
type MetricType entity.MetricType

// getMetricType returns the metric type
func (t *MetricType) getMetricType() entity.MetricType {
	return entity.MetricType(*t)
}
