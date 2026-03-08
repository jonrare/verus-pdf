package main

import (
	"context"

	"veruspdf/backend/annotate"
	"veruspdf/backend/convert"
	"veruspdf/backend/bookmarks"
	"veruspdf/backend/edit"
	"veruspdf/backend/forms"
	"veruspdf/backend/merge"
	"veruspdf/backend/ocr"
	"veruspdf/backend/optimize"
	"veruspdf/backend/security"
	"veruspdf/backend/viewer"
)

type App struct {
	ctx context.Context

	MergeService    *merge.Service
	SecurityService *security.Service
	OptimizeService *optimize.Service
	ViewerService   *viewer.Service
	AnnotateService *annotate.Service
	FormsService    *forms.Service
	ConvertService  *convert.Service
	OCRService      *ocr.Service
	BookmarksService *bookmarks.Service
	EditService      *edit.Service
}

func NewApp() *App {
	return &App{
		MergeService:    merge.New(),
		SecurityService: security.New(),
		OptimizeService: optimize.New(),
		ViewerService:   viewer.New(),
		AnnotateService: annotate.New(),
		FormsService:    forms.New(),
		ConvertService:  convert.New(),
		OCRService:      ocr.New(),
		BookmarksService: &bookmarks.Service{},
		EditService:      edit.New(),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.ViewerService.SetContext(ctx)
	a.ConvertService.SetContext(ctx)
}

func (a *App) shutdown(ctx context.Context) {}
