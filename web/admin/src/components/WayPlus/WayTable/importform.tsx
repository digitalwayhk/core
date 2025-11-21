import { InboxOutlined } from '@ant-design/icons';
import { Alert, Button, Card, Col, message, Modal, Progress, Row, Space } from 'antd';
import React, { useEffect, useState } from 'react';
import WayStepFrom from '../WayForm/stepform';
import WayTable from '.';
import type { ModelAttribute, SearchItem, TableData, WayFieldAttribute } from '../way';
import WayEditTable from '../WayForm/edittable';
import * as XLSX from 'xlsx';
import Dragger from 'antd/lib/upload/Dragger';
import type { FormPlus } from '../WayForm';
import { exportExcel } from './exportform';
import type { StepProps } from 'antd/lib/steps';

interface ImportFormProps {
  title?: string;
  isShow: boolean;
  attr: ModelAttribute;
  onAdd?: any;
  form?: FormPlus;
  onShowChange?: (isshow: boolean) => void;
  onSearchData?: (item: SearchItem, callback: (data: TableData) => void) => void;
}

const ImportForm: React.FC<ImportFormProps> = (props) => {
  const [currentStep, setCurrentStep] = useState<number>(0);
  const [isshow, setModalShow] = useState(props.isShow ?? false);
  const [rowcount, setRowCount] = useState(0);
  const [sourceTable, setSourceTable] = useState({
    model: undefined,
    data: { rows: [], total: 0 },
  });
  const [targetFields, setTargetFields] = useState<WayFieldAttribute[]>(filterfields(props.attr));
  const [importState, setImportState] = useState({
    message: '',
    description: '',
    type: 'success',
    count: 0,
  });
  const [upfileResult, setUpfileResult] = useState(null);
  const [sourceTotargetMapTable, setSourceTotargetMapTable] = useState({
    model: null,
    rows: [],
    total: 0,
  });
  const [stopImport, setStopImport] = useState(true);
  const [fieldMap, setFmap] = useState(undefined);

  function clearstate() {
    setCurrentStep(0);
    setRowCount(0);
    setSourceTable({ model: undefined, data: { rows: [], total: 0 } });
    setImportState({
      message: '',
      description: '',
      type: 'success',
      count: 0,
    });
    setUpfileResult(null);
    setSourceTotargetMapTable({
      model: null,
      rows: [],
      total: 0,
    });
    setStopImport(true);
    setFmap(undefined);
  }
  function filterfields(attr) {
    return attr.fields?.filter((field) => {
      return field.visible && !field.disabled;
    });
  }
  useEffect(() => {
    setModalShow(props.isShow);
  }, [props.isShow]);
  useEffect(() => {
    setTargetFields(filterfields(props.attr));
  }, [props.attr]);

  const waitTime = (time: number = 100) => {
    return new Promise((resolve) => {
      setTimeout(() => {
        resolve(true);
      }, time);
    });
  };
  function stepItem() {
    const step1: StepProps = { title: '上传导入文件' };
    const step2: StepProps = { title: '设置导入映射' };
    const step3: StepProps = { title: '导入数据' };
    const items: StepProps[] = [step1, step2, step3];
    return items;
  }
  function getattr(table: any[]): ModelAttribute {
    const attr: ModelAttribute = { fields: [] };
    if (table.length > 0) {
      for (const n in table[0]) {
        attr.fields?.push({ field: n, title: n, type: 'string', visible: true, sorter: false });
      }
    }
    return attr;
  }
  function setshowtable(result) {
    // eslint-disable-next-line @typescript-eslint/no-shadow
    const { rowcount, data } = result;
    if (rowcount > 0) {
      setRowCount(rowcount);
      const m = getattr(data);
      const rows = [];
      for (let i = 0; i < 6; i++) {
        rows.push(data[i]);
      }
      setSourceTable({ model: m, data: { rows: rows, total: 5 } });
    }
  }

  function mapFileToData() {
    //if (sourceTotargetMapTable.model != null) return
    const sourceItems = new Map<number, string>();
    const targetItems = new Map<number, string>();
    sourceTable.model?.fields?.forEach((field, index) => {
      sourceItems.set(index, field.title);
    });
    const data: any = [];
    targetFields.forEach((field, index) => {
      targetItems.set(index, field.title);
      const fm = field.foreign && field.foreign.isfkey ? 'Name' : '';
      const sourcefield = sourceTable.model?.fields?.find((sf) => {
        return sf.field == field.field || sf.field == field.title;
      });
      let sindex = undefined;
      if (sourcefield == undefined || sourcefield == null) {
        if (index < sourceTable.model?.fields.length) sindex = index;
        data.push({ id: index, source: sindex, target: index, defaultValue: '', foreignMap: fm });
      } else {
        for (let item of sourceItems.entries()) {
          if (sourcefield.title == item[1] || sourcefield.field == item[1]) {
            sindex = item[0];
            break;
          }
        }
        data.push({ id: index, source: sindex, target: index, defaultValue: '', foreignMap: fm });
      }
    });
    const items = [
      {
        field: 'source',
        title: '来源列名',
        type: 'string',
        comvtp: { isvtp: true, items: sourceItems },
        visible: true,
        sorter: false,
      },
      {
        field: 'target',
        title: '目标列名',
        type: 'string',
        comvtp: { isvtp: true, items: targetItems },
        visible: true,
        sorter: false,
        isedit: false,
      },
      { field: 'defaultValue', title: '默认值', type: 'string', visible: true, sorter: false },
      { field: 'foreignMap', title: '外关联映射', type: 'string', visible: true, sorter: false },
    ];
    setSourceTotargetMapTable({
      model: { fields: items, isadd: false, isedit: true, isremove: false },
      rows: data,
      total: data.length,
    });
  }

  function mapExcelToData(excelRow) {
    const row = {};
    for (let ii in sourceTotargetMapTable.rows) {
      var stmap = sourceTotargetMapTable.rows[ii];
      var sfield = sourceTable.model?.fields[stmap.source];
      if ((sfield == undefined || sfield == null) && stmap.defaultValue == '') continue;
      var titem = targetFields[stmap.target];
      if (titem == undefined || titem == null) continue;
      if (stmap.defaultValue != null && stmap.defaultValue != '') {
        row[titem.field] = stmap.defaultValue;
        if (titem.foreign?.isfkey) {
          row[titem.field] = stmap[titem.field];
        }
        continue;
      }
      var sn = sfield.field;
      var value = excelRow[sn];
      if (titem.required) {
        if (value == undefined || value == null || value == '') {
          importError(
            row,
            `导入数据${sfield.field}为空值，映射项${titem.title}不能为空，请修改Excel后重试！`,
            0,
          );
          return;
        }
      }
      if (value == undefined || value == null || value == '') {
        continue;
      }
      if (titem?.comvtp && titem.comvtp.isvtp) {
        for (const item of titem.comvtp.items.entries()) {
          if (value == item[1]) {
            value = item[0];
            break;
          }
        }
      }
      if (titem.foreign?.isfkey && fieldMap != undefined) {
        if (fieldMap.has(titem.field)) {
          let vnmap = fieldMap.get(titem.field);
          value = vnmap[value];
        }
      }
      row[titem.field] = value;
    }
    return row;
  }
  function getForeignValueMap(excelRows) {
    const fmap = new Map<string, Map<string, string>>();
    sourceTotargetMapTable.rows.forEach((stmap) => {
      const sfield = sourceTable.model?.fields[stmap.source];
      if (sfield) {
        const field = targetFields[stmap.target];
        const fname = stmap.foreignMap == '' ? 'Name' : stmap.foreignMap;
        if (field.foreign?.isfkey && stmap.defaultValue == '') {
          if (!fmap.has(field.field)) fmap.set(field.field, new Map<string, string>());
          const vmap = fmap.get(field.field);
          for (const r in excelRows) {
            const row = excelRows[r];
            const value = row[sfield.field];
            if (value != null && value != '' && !vmap.has(value)) {
              vmap?.set(value, fname);
            }
          }
        }
      }
    });
    return fmap;
  }
  function searchForeignValueMap(excelRows) {
    const fmap = getForeignValueMap(excelRows);
    if (fmap.size == 0) {
      setStopImport(false);
      setImportState({
        message: '请点击开始导入',
        description: '导入数据准备完成......',
        type: 'info',
        count: 0,
      });
      return;
    }
    let errmsg = '';
    fmap.forEach((vnmap, key) => {
      const field = targetFields.find((f) => f.field == key);
      const symbol = targetFields
        .find((f) => f.type == 'string')
        .searchsymbol.find((f) => f.label == '等于').value;
      var list = [];
      var fv = '';
      vnmap.forEach((v, k) => {
        fv = v.toLowerCase();
        list.push({ name: v, symbol: symbol, value: k, relation: '||' });
      });
      const searchItem = { field: field, foreign: field.foreign, whereList: list };
      if (props.onSearchData) {
        waitTime(200);
        props.onSearchData(searchItem, (data) => {
          if (data && data.rows.length > 0) {
            if (data.rows.length == vnmap.size) {
              console.log(vnmap);
              for (const r in data.rows) {
                const rowname = data.rows[r][fv];
                if (vnmap.has(rowname)) vnmap[rowname] = data.rows[r].id;
              }
              setFmap(fmap);
              setStopImport(false);
              console.log(importState.description);
              setImportState({
                message: '请点击开始导入',
                description: `导入数据准备完成,已映射${field?.title}数据${data.total}项。\n`,
                type: 'info',
                count: 0,
              });
            } else {
              vnmap.forEach((vv, kk) => {
                var row = data.rows.find((r) => r[fv] == kk);
                errmsg += `${field?.title}中未找到${fv}为${kk}的数据，请修改Excel后重试!\n`;
                if (row == null) {
                  importError(data.rows, errmsg, 0);
                }
              });
            }
          } else {
            errmsg += `${field?.title}中未找到${fv}为${list[0].value}的数据，请修改Excel后重试!\n`;
            importError(data.rows, errmsg, 0);
          }
        });
      }
    });
  }

  function startImportData(count) {
    if (upfileResult == null || upfileResult.data == undefined) {
      Modal.error({
        content: <div>导入数据未上传，不能导入！</div>,
      });
      return;
    }
    const { data } = upfileResult;
    if (sourceTotargetMapTable.rows.length <= 0) {
      Modal.error({
        content: <div>Excel于数据映射关系未设置不能导入！</div>,
      });
      return;
    }
    if (stopImport) return;
    if (count == data.length) {
      setImportState({ message: '导入数据完成', description: '', type: 'success', count: count });
      setStopImport(true);
      return;
    }
    const row = mapExcelToData(data[count]);
    if (row != null) {
      submitData(row, (success, err) => {
        backcall(row, success, err, count);
      });
    }
  }
  function backcall(row, success, err, count) {
    if (success) {
      count++;
      setImportState({
        message: '导入成功',
        description: JSON.stringify(row),
        type: 'success',
        count: count,
      });
      waitTime(200);
      startImportData(count);
    } else {
      importError(row, err, count);
    }
  }
  async function submitData(row: any, callback) {
    if (props.onAdd) {
      props.onAdd('add', row).then(async (result) => {
        console.log(result);
        if (result.ok != undefined && result.ok == false) {
          setStopImport(true);
          callback(false, result.statusText);
        }
        if (result != undefined && result.success) {
          await waitTime(200);
          callback(true);
        } else {
          setStopImport(true);
          callback(false, result.message);
        }
      });
    }
  }

  function importError(row, message, count) {
    const prop = {
      message: '导入出错:' + JSON.stringify(row),
      description: message,
      type: 'error',
      count: count,
      action: (
        <Space>
          <Button
            size="small"
            type="ghost"
            onClick={() => {
              if (props.form) {
                const form = props.form;
                form.clear();
                form.setHideSearch(true);
                form.setTitle(props.title);
                form.show();
                form.setValues(row);
                form.onFinish = (values) => {
                  console.log(values);
                  form.hide();
                  submitData(values, (success, err) => {
                    backcall(values, success, err, count);
                  });
                };
              }
            }}
          >
            修改
          </Button>
        </Space>
      ),
    };
    setImportState(prop);
  }
  function renderUpfile() {
    const upfile = {
      name: 'file',
      accept: '.xlsx, .xls',
      onChange(info: any) {
        console.log(info);

        // 通过FileReader对象读取文件
        const fileReader = new FileReader();
        fileReader.onload = (event) => {
          try {
            const { result } = event.target;
            // 以二进制流方式读取得到整份excel表格对象
            const workbook = XLSX.read(result, { type: 'binary' });
            // 存储获取到的数据
            let data: any = [];
            // 遍历每张工作表进行读取（这里默认只读取第一张表）
            for (const sheet in workbook.Sheets) {
              // esline-disable-next-line
              if (workbook.Sheets.hasOwnProperty(sheet)) {
                // 利用 sheet_to_json 方法将 excel 转成 json 数据
                data = XLSX.utils.sheet_to_json(workbook.Sheets[sheet]);
                break; // 如果只取第一张表，就取消注释这行
              }
            }
            // 最终获取到并且格式化后的 json 数据
            const res = { data: data, rowcount: data.length };
            setUpfileResult(res);
            setshowtable(res);
          } catch (e) {
            // 这里可以抛出文件类型错误不正确的相关提示
            message.error('发生错误！');
            console.log(e);
          }
        };
        // 以二进制方式打开文件
        fileReader.readAsBinaryString(info.file.originFileObj);
      },
    };
    return (
      <Card>
        <Row gutter={[8, 8]}>
          <Col span={24}>
            <Alert
              showIcon
              message={`上传文件中的数据为${rowcount}行`}
              type={'info'}
              action={
                <Space>
                  <Button
                    size="small"
                    type="primary"
                    onClick={() => {
                      const columns: any[] = [];
                      const row = {};
                      targetFields.forEach((item) => {
                        columns.push({ title: item.title, key: item.field });
                        row[item.field] = item.type + '(数据类型提示，录入数据时请删除)';
                      });
                      exportExcel(columns, [row], props.title + '导入模板.xlsx');
                    }}
                  >
                    下载导入模板
                  </Button>
                </Space>
              }
            />
          </Col>
        </Row>
        <Row gutter={[8, 8]}>
          <Col span={24}>
            <Dragger {...upfile}>
              <p className="ant-upload-drag-icon">
                <InboxOutlined />
              </p>
              <p className="ant-upload-text">点击或拖拽文件上传</p>
            </Dragger>
          </Col>
        </Row>
        <Row gutter={[8, 8]}>
          <Col span={24}>
            <WayTable
              isclosecard={true}
              attr={sourceTable.model}
              data={sourceTable.data}
             />
          </Col>
        </Row>
        <Row gutter={[8, 8]}>
          <Col span={24} push={11}>
            <Button
              type="primary"
              disabled={rowcount < 1}
              onClick={() => {
                setCurrentStep(currentStep + 1);
              }}
            >
              下一步
            </Button>
          </Col>
        </Row>
      </Card>
    );
  }
  function renderMapTable() {
    const prop = {
      isselect: false,
      closesearch: true,
      closetoolbar: true,
      closecard: true,
      model: sourceTotargetMapTable.model,
      data: { rows: sourceTotargetMapTable.rows, total: sourceTotargetMapTable.total },
      onDataChange: (data: any) => {
        setSourceTotargetMapTable({
          ...sourceTotargetMapTable,
          rows: data.rows,
        });
      },
      onGetFieldToEdit: (field: WayFieldAttribute, row: any) => {
        if (field.field == 'defaultValue') {
          const index = row.target;
          const tfield = targetFields[index];
          return tfield;
        }
        return field;
      },
      onEditRowing: (row: any, field: string, value: any) => {
        if (value != undefined) row.defaultValue = String(value);
        return true;
      },
      onSearchValueChange: (field: WayFieldAttribute, row: any, foreignvalue: any) => {
        if (foreignvalue.text != undefined) row.defaultValue = foreignvalue.text;
      },
      onSearchData: props.onSearchData,
    };
    return (
      <>
        <Row gutter={[16, 16]}>
          <Col span={24}>
            <WayEditTable {...prop} />
          </Col>
        </Row>
        <Row gutter={[16, 16]}>
          <Col span={12} push={10}>
            <Button
              type="primary"
              onClick={() => {
                setCurrentStep(currentStep + 1);
              }}
            >
              下一步
            </Button>
          </Col>
          <Col span={12} push={1}>
            <Button
              onClick={() => {
                setCurrentStep(currentStep - 1);
              }}
            >
              上一步
            </Button>
          </Col>
        </Row>
      </>
    );
  }
  function renderImportData() {
    return (
      <>
        <Row gutter={[16, 16]}>
          <Col span={24}>
            <Alert
              message={`数据导入，总需要导入${rowcount}行，已导入${importState.count}行`}
              type="info"
            />
          </Col>
        </Row>
        <Row gutter={[16, 16]}>
          <Col span={24}>
            <Progress
              percent={parseInt(((importState.count / rowcount) * 100).toString())}
              status="active"
            />
          </Col>
        </Row>
        <Row gutter={[16, 16]}>
          <Col span={24}>
            <Alert {...importState} />
          </Col>
        </Row>
        <Row gutter={[8, 8]}>
          <Col span={12} push={10}>
            <Button
              type="primary"
              disabled={stopImport}
              onClick={() => {
                startImportData(importState.count);
              }}
            >
              开始导入
            </Button>
          </Col>
          <Col span={12} push={1}>
            <Button
              onClick={() => {
                setCurrentStep(currentStep - 1);
              }}
            >
              上一步
            </Button>
          </Col>
        </Row>
      </>
    );
  }
  function close(show) {
    setModalShow(show);
    if (props.onShowChange) props.onShowChange(show);
    if (!show) {
      clearstate();
    }
  }
  function render() {
    return (
      <WayStepFrom
        title={`导入${props.title}数据`}
        closeOk={true}
        isModal={true}
        isShow={isshow}
        stepItem={stepItem()}
        currentStep={currentStep}
        onChange={(current) => {
          if (current == 1) mapFileToData();
          if (current == 2) {
            if (upfileResult && upfileResult.data) {
              const { data } = upfileResult;
              searchForeignValueMap(data);
            }
          }
          setCurrentStep(current);
        }}
        onCurrentStepComponent={(current) => {
          if (current == 0) return renderUpfile();
          if (current == 1) return renderMapTable();
          if (current == 2) return renderImportData();
          return <></>;
        }}
        onShowChange={(show) => {
          // if (show == false && (importState.count > 0 && importState.count != rowcount)) {
          //     Modal.confirm({
          //         title: '确定关闭导入吗？',
          //         icon: <ExclamationCircleOutlined />,
          //         content: "数据导入正在进行中，关闭后将无法继续。",
          //         onOk() {
          //             close(show)
          //         }
          //     })
          // }
          close(show);
        }}
       />
    );
  }
  return render();
};
export default ImportForm;
