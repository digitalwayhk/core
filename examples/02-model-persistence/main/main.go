package main

import (
	"fmt"

	"github.com/digitalwayhk/core/examples/02-model-persistence/models"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	pt "github.com/digitalwayhk/core/pkg/persistence/types"
)

func main() {
	// ModelList 是业务层最常用的数据入口。业务代码只表达“新增、查询、保存”，
	// 具体使用 SQLite、MySQL、Redis 等由持久化层和配置决定。
	list := entity.NewModelList[models.Book](nil)

	// NewItem 会创建 *Book，并调用 Book.NewModel 初始化内嵌 entity.Model。
	book := list.NewItem()
	book.Title = "Core 入门"
	book.Author = "DigitalWay"
	book.Category = "framework"

	// Add 只把对象加入待保存队列；Save 才会真正调用底层 IDataAction。
	if err := list.Add(book); err != nil {
		panic(err)
	}
	if err := list.Save(); err != nil {
		panic(err)
	}

	// SearchWhere 是常用快捷查询。复杂查询可直接操作 SearchItem。
	rows, total, err := list.SearchAll(1, 10, func(item *pt.SearchItem) {
		item.AddWhereN("Category", "framework")
		item.AddSortN("ID", true)
	})
	if err != nil {
		panic(err)
	}

	fmt.Printf("保存书籍 ID=%d，查询到 %d 条，当前页返回 %d 条\n", book.GetID(), total, len(rows))
}
