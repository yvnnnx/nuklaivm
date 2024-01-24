// Copyright (C) 2023, AllianceBlock. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import "errors"

var (
	ErrTxNotFound    = errors.New("tx not found")
	ErrAssetNotFound = errors.New("asset not found")
	ErrOrderNotFound = errors.New("order not found")
)
