package sqlite

import outport "github.com/PhilHem/drillip/internal/application/port/out"

var _ outport.Repository = (*Store)(nil)
var _ outport.MaintenanceStore = (*Store)(nil)
