import React, { useEffect, useState } from 'react';
import * as XLSX from 'xlsx';
import { DefaultRowToDisplay } from './index';


export function exportExcel(columns: any, rows: any, fileName: string) {
    const _headers = columns
        .map((item, i) => Object.assign({}, { key: item.key, title: item.title, position: String.fromCharCode(65 + i) + 1 }))
        .reduce((prev, next) => Object.assign({}, prev, { [next.position]: { key: next.key, v: next.title } }), {});

    const _data = rows
        .map((item, i) => columns.map((key, j) => Object.assign({}, { content: item[key.key], position: String.fromCharCode(65 + j) + (i + 2) })))
        // 对刚才的结果进行降维处理（二维数组变成一维数组）
        .reduce((prev, next) => prev.concat(next))
        // 转换成 worksheet 需要的结构
        .reduce((prev, next) => Object.assign({}, prev, { [next.position]: { v: next.content } }), {});

    // 合并 headers 和 data
    const output = Object.assign({}, _headers, _data);
    // 获取所有单元格的位置
    const outputPos = Object.keys(output);
    // 计算出范围 ,["A1",..., "H2"]
    const ref = `${outputPos[0]}:${outputPos[outputPos.length - 1]}`;

    // 构建 workbook 对象
    const wb = {
        SheetNames: ['mySheet'],
        Sheets: {
            mySheet: Object.assign(
                {},
                output,
                {
                    '!ref': ref,
                    '!cols': [{ wpx: 45 }, { wpx: 100 }, { wpx: 200 }, { wpx: 80 }, { wpx: 150 }, { wpx: 100 }, { wpx: 300 }, { wpx: 300 }],
                },
            ),
        },
    };

    // 导出 Excel
    XLSX.writeFile(wb, fileName)
}
export async function pageExportExcel(model, total, searchItem, search, filename) {
    if (total > 0) {
        var columns = []
        model.fields.forEach((field) => {
            if (field.visible)
                columns.push({ title: field.title, key: field.field, item: field })
        })
        var item = Object.assign({}, searchItem)
        item.page = 1
        item.size = total
        if (item.size > 0) {
            search(item).then(result => {
                if (result != undefined && result.success) {
                    if (result.data && result.data.rows) {
                        var rows = result.data.rows
                        for (var r in rows) {
                            for (var c in columns) {
                                var value = rows[r][columns[c].key]
                                var dv = DefaultRowToDisplay(value, columns[c].item, rows[r])
                                rows[r][columns[c].key] = dv ?? ""
                            }
                        }
                        exportExcel(columns, rows, filename)
                    }
                }
            })
        }
    }
}