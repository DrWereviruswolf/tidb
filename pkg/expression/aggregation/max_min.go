// Copyright 2017 PingCAP, Inc.
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

package aggregation

import (
	"github.com/pingcap/tidb/pkg/sessionctx/stmtctx"
	"github.com/pingcap/tidb/pkg/types"
	"github.com/pingcap/tidb/pkg/util/chunk"
	"github.com/pingcap/tidb/pkg/util/collate"
)

type maxMinFunction struct {
	aggFunction
	isMax bool
	ctor  collate.Collator
}

// GetResult implements Aggregation interface.
func (*maxMinFunction) GetResult(evalCtx *AggEvaluateContext) (d types.Datum) {
	return evalCtx.Value
}

// GetPartialResult implements Aggregation interface.
func (mmf *maxMinFunction) GetPartialResult(evalCtx *AggEvaluateContext) []types.Datum {
	return []types.Datum{mmf.GetResult(evalCtx)}
}

// Update implements Aggregation interface.
func (mmf *maxMinFunction) Update(evalCtx *AggEvaluateContext, sc *stmtctx.StatementContext, row chunk.Row) error {
	a := mmf.Args[0]
	value, err := a.Eval(row)
	if err != nil {
		return err
	}
	if evalCtx.Value.IsNull() {
		value.Copy(&evalCtx.Value)
	}
	if value.IsNull() {
		return nil
	}
	var c int
	c, err = evalCtx.Value.Compare(sc, &value, mmf.ctor)
	if err != nil {
		return err
	}
	if (mmf.isMax && c == -1) || (!mmf.isMax && c == 1) {
		value.Copy(&evalCtx.Value)
	}
	return nil
}
