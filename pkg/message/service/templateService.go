package service

import (
	"errors"
	IModel "github.com/digitalwayhk/core/models"
	"github.com/digitalwayhk/core/pkg/message/models"
	"log"
)

var templateService *TemplateService

type TemplateService struct {
	tList *IModel.ModelList[models.MsgTemplate]
	cList *IModel.ModelList[models.MsgContent]
}

func init() {
	templateService = &TemplateService{
		tList: IModel.NewManageModelList[models.MsgTemplate](),
		cList: IModel.NewManageModelList[models.MsgContent](),
	}
}

func GetTemplateService() *TemplateService {
	return templateService
}

func (own *TemplateService) reloadTemplate() {
	tItem := own.tList.GetSearchItem()
	tItem.AddWhereN("IsDisable", false)
	err := own.tList.LoadList(tItem)
	if err != nil {
		log.Println(err)
	}
}
func (own *TemplateService) reloadContent() {
	cItem := own.cList.GetSearchItem()
	cItem.AddWhereN("IsDisable", false)
	err := own.cList.LoadList(cItem)
	if err != nil {
		log.Println(err)
	}
}

func (own *TemplateService) GetTemplate(id uint) (*models.MsgTemplate, error) {
	if id == 0 {
		return nil, errors.New("id is zero")
	}
	_, cm := own.tList.FindOne(func(o *models.MsgTemplate) bool {
		return o.ID == id
	})
	return cm, nil
}
func (own *TemplateService) GetContent(templateId uint, smsType int) (*models.MsgContent, error) {
	if templateId == 0 {
		return nil, errors.New("id is zero")
	}
	_, cm := own.cList.FindOne(func(o *models.MsgContent) bool {
		return o.TemplateId == templateId && o.Type == smsType
	})
	return cm, nil
}
