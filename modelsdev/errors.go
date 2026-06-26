package modelsdev

import "errors"

// ErrModelsSchema signals that models.dev data does not match the expected
// shape: either the top-level maps are empty (gross drift, on every fetch) or a
// requested provider carries a malformed model (per-model, in Provider and
// Models). models.dev is unversioned community JSON, so validation is the only
// signal of drift; this error makes that drift loud rather than degrading
// enrichment to silent blanks. The model-resolution sentinels (ErrModelAmbiguous,
// ErrModelNotFound) belong to the root package and are not defined here.
var ErrModelsSchema = errors.New("models.dev schema unrecognised")
