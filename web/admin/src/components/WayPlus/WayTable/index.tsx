import React, { useEffect, useState, useCallback, useMemo, memo } from 'react';
import { Button, Card, Table, Tabs, Tooltip } from 'antd';
import type { ChildModelAttribute, WayFieldAttribute, SearchItem, TableData } from '../way';
import type {
  ExpandableConfig,
  SorterResult,
  TablePaginationConfig,
} from 'antd/lib/table/interface';
import { isArray } from 'lodash';
import WayTextBox from '../WayTextBox';
import dayjs from 'dayjs';
import { AnyToTypeData } from '../waymodel';

export function DefaultRowToDisplay(value: any, item: WayFieldAttribute, row: any) {
  let data = AnyToTypeData(item, value)
  if (item.comvtp?.isvtp && item.type != 'string') {
    //if (item.type != 'strin
    const mmap: Map<number, string> = new Map(item.comvtp.items);
    data = mmap.get(data);
    //}
  }
  if (item.foreign?.isfkey) {
    const oofield = item.foreign.oneobjectfield;
    const odname = item.foreign.onedisplayname;
    if (row[oofield] != undefined) {
      data = row[oofield][odname];
    }
  }
  if (item.type == 'boolean') data = data ? '是' : '否';
  if (item.type == 'datetime') {
    if (data == '' || data == null) return data;
    let fdata = item.datatimetype?.dateformat;
    if (item.datatimetype?.istime) {
      fdata = item.datatimetype?.dateformat + " " + item.datatimetype?.timeformat
    }
    data = dayjs(data).format(fdata);
  }
  return data;
}
const getPropName = (props: WayTableProps, field: string): string => {
  const attr = props.attr;
  const porp = attr?.fields?.find((item) => item.field == field)?.porpfield ?? field;
  return porp;
}
const getColumns = (props: WayTableProps, rowedit: boolean, editrow, setEditRow) => {
  const attr = props.attr;
  const cols: any = [];
  function columnToDisplay(text: any, item: WayFieldAttribute, record: any) {
    // eslint-disable-next-line @typescript-eslint/no-shadow
    const data = DefaultRowToDisplay(text, item, record);
    if (props.onFieldRender != undefined) {
      return props.onFieldRender(item, data, record);
    }
    return <Tooltip title={data}>{data}</Tooltip>;
  }
  const columnToEdit = (item: any, record: any) => {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    if (props.onColumnToEdit) {
      return props.onColumnToEdit(item, record);
    }
    if (props.onGetFieldToEdit) {
      // eslint-disable-next-line no-param-reassign
      item = props.onGetFieldToEdit(item, record);
    }
    setEditRow(record);
    return (
      <WayTextBox
        attr={item}
        value={editrow[item.field]}
        onChange={(value) => {
          if (props.onRowDataChangeing != undefined) {
            if (!props.onRowDataChangeing(record, item.field, value)) return;
          }
          console.log(value);
          const res = Object.assign({}, editrow);
          record[item.field] = value;
          res[item.field] = value;
          setEditRow(res);
        }}
        onSearchValueChange={(obj) => {
          if (props.onSearchValueChange) {
            props.onSearchValueChange(item, record, obj);
          }
        }}
        onSearchBefore={(item: SearchItem, callback) => {
          if (props.onSearchData) {
            props.onSearchData(item, (data) => {
              callback(data.model, data);
            });
          }
        }}
        onSearchData={props.onSearchData}
      />
    );
  };
  attr?.fields
    ?.filter((field) => field.visible)
    .forEach((item) => {
      cols.push({
        dataIndex: item.field,
        title: item.title,
        sorter: item.sorter ?? true,
        ellipsis: true,
        render: (text: any, record: any) => {
          if (record == undefined) return;
          if (rowedit && record.editable && (item.isedit == undefined || item.isedit == true)) {
            return columnToEdit(item, record);
          } else {
            return columnToDisplay(text, item, record);
          }
        },
      });
    });
  return cols;
};
export interface WayTableProps {
  attr?: ChildModelAttribute;
  data?: TableData | null;
  loading?: boolean;
  isselect?: boolean;
  selectType?: string | 'checkbox' | 'radio';
  isedit?: boolean;
  isexpandable?: boolean;
  rowedit?: boolean;
  isclosecard?: boolean;
  current?: number;
  onExpandable?: (attr: ChildModelAttribute) => ExpandableConfig<any>;
  onFieldRender?: (field: WayFieldAttribute, text: any, record: any) => JSX.Element;
  onSearchData?: (item: SearchItem, callback: (data: TableData) => void) => void;
  onSelectRows?: (row: any | null, selectedRows: any[], selected: boolean) => void;
  onSelectStateChange?: (ischeck: boolean) => void;
  onRowClick?: (event: React.MouseEvent<HTMLElement, MouseEvent>, row: object) => void;
  onRowDoubleClick?: (event: React.MouseEvent<HTMLElement, MouseEvent>, row: object) => void;
  onRowMouseEnter?: (event: React.MouseEvent<HTMLElement, MouseEvent>, row: object) => void;
  onMouseLeave?: (event: React.MouseEvent<HTMLElement, MouseEvent>, row: object) => void;
  onRowDataChangeing?: (row: any, field: string, value: any) => boolean;
  onExpandedRowTabPane?: (childmodel: ChildModelAttribute, record: any) => JSX.Element;
  onColumnToEdit?: (field: WayFieldAttribute, row: any) => JSX.Element;
  onGetFieldToEdit?: (field: WayFieldAttribute, row: any) => WayFieldAttribute;
  onSearchValueChange?: (field: WayFieldAttribute, row: any, foreignvalue: any) => void;
}

const TabPane = Tabs.TabPane;

const WayTable: React.FC<WayTableProps> = (props) => {

  const [loading, setLoading] = useState(props.loading ?? false);
  const [data, setData] = useState(props.data ?? { rows: [], total: 0 });
  const [rowedit, setRowedit] = useState(props.rowedit ?? false);
  const [current, setCurrent] = useState(props.current ?? 1);
  const [currentSize, setCurrentSize] = useState(10);
  const [selectedRowKeys, setSelectedRowKeys] = useState<any[]>([]);
  const [selectType, setSelectType] = useState<string | 'checkbox' | 'radio'>(
    props.selectType ?? 'radio',
  );
  const [selectTitle, setSelectTitle] = useState<JSX.Element | null>();
  //props.selectType != null && props.selectType == 'radio' ? radiobutton() : null,
  const [editrow, setEditRow] = useState({});
  useEffect(() => {
    setData(props.data ?? { rows: [], total: 0 });
    // eslint-disable-next-line @typescript-eslint/no-use-before-define
    //setSelectedRowKeys(selectedRowKeys);
  }, [props.data]);
  useEffect(() => {
    setRowedit(props.rowedit ?? false);
  }, [props.rowedit]);
  useEffect(() => {
    setLoading(props.loading ?? false);
  }, [props.loading]);
  useEffect(() => {
    setCurrent(props.current ?? 1);
  }, [props.current]);

  const handleTableChange = (
    pagination: TablePaginationConfig,
    filters: Record<string, any[] | null>,
    sorter: SorterResult<any> | SorterResult<any>[],
  ) => {
    const setLoaded = (data: TableData) => {
      setLoading(false);
      setData(data);
    };
    const getsort = (sort: SorterResult<any>): string => {
      if (sort.column == undefined) return '';
      const ss = { name: getPropName(props, sort.field), isdesc: false };
      if (sort.order == 'descend') ss.isdesc = true;
      return ss;
    };
    setLoading(true);
    setCurrent(pagination.current ?? 1);
    setCurrentSize(pagination.pageSize ?? 10);
    try {
      const item: SearchItem = {
        page: pagination.current ?? 1,
        size: pagination.pageSize ?? 10,
        sortList: [],
      };
      if (sorter != undefined) {
        if (isArray(sorter)) {
          sorter.forEach((item) => {
            const s = getsort(item);
            if (s != '') item.sortList.push(s);
          });
        } else {
          const s = getsort(sorter);
          if (s != '') item.sortList.push(s);
        }
      }
      if (props.onSearchData != undefined) {
        props.onSearchData(item, setLoaded);
      }
    } finally {
      setLoading(false);
    }
  };

  const onSelectChange = (keys: any[], selectedRows: Object[]) => {
    const selected = selectedRowKeys.length <= keys.length;
    setSelectedRowKeys(keys);
    if (props.onSelectRows != undefined) {
      if (keys.length > 0) {
        let row: any = selectedRows[selectedRows.length - 1];
        if (!selected) {
          row = null;
          if (keys.length == 1) {
            row = data.rows.find((r) => r.id == keys[0]);
          }
        }
        props.onSelectRows(row, keys, selected);
      } else {
        props.onSelectRows(null, keys, false);
      }
    }
  };
  const rowSelection = () => {
    if (!props.isselect) return undefined;
    return {
      selectedRowKeys: selectedRowKeys,
      fixed: true,
      type: selectType,
      columnTitle: selectTitle,
      hideSelectAll: false,
      columnWidth: '32px',
      // selections: [
      //   {
      //     key: 'invert',
      //     text: '反选',
      //     onSelect: (changeableRowKeys: any[]) => {
      //       const invertKeys = changeableRowKeys.filter((key) => {
      //         return !selectedRowKeys.includes(key);
      //       });
      //       const rows = data.rows.filter((item) => invertKeys.includes(item.id));
      //       onSelectChange(invertKeys, rows);
      //     },
      //   },
      //   {
      //     key: 'CheckboxOrRadio',
      //     text: '切换单选',
      //     onSelect: () => {
      //       getSelectType(selectType);
      //     },
      //   },
      // ],
      onChange: onSelectChange,
    };
  };
  function getSelectType(type: string) {
    if (type == 'checkbox') {
      setSelectType('radio');
      setSelectedRowKeys([]);
      if (props.onSelectStateChange) props.onSelectStateChange(false);
      setSelectTitle(radiobutton());
    }
  }
  function radiobutton(): JSX.Element {
    return (
      <Button
        type={'link'}
        onClick={() => {
          setSelectTitle(null);
          setSelectType('checkbox');
          setSelectedRowKeys([]);
          if (props.onSelectStateChange) props.onSelectStateChange(true);
        }}
      >
        多选
      </Button>
    );
  }
  function setrowedit(record) {
    if (props.isedit) {
      record.editable = !rowedit;
      const rows = [...data.rows];
      rows.forEach((r) => {
        if (r.id == record.id) {
          r.editable = !rowedit;
        } else {
          if (r.editable) r.editable = false;
        }
      });
      setRowedit(!rowedit);
    }
  }
  function expandable(): ExpandableConfig<any> | undefined {
    if (
      props.isexpandable &&
      props?.attr?.childmodels != undefined &&
      props.attr.childmodels.length > 0
    ) {

      if (props.onExpandable == undefined) {
        return {
          onExpand: (expanded: any, record: any) => {
            if (!expanded) return;
            onRowSelect(record, "id");
            if (props?.attr?.childmodels != undefined && props.attr.childmodels.length > 0) {
              const cm = props?.attr?.childmodels[0];
              if (!record[cm.propertyname]) {
                record[cm.propertyname] = [];
              }
              record[cm.propertyname].total = record[cm.propertyname].length;
              if (record[cm.propertyname].total == 0) {
                const item: SearchItem = { page: 1, size: 10 };
                getchildTable(item, cm, record);
              }
            }
          },
          // eslint-disable-next-line @typescript-eslint/no-use-before-define
          expandedRowRender: expandedRowRender,
        };
      } else {
        return props.onExpandable(props.attr);
      }
    }
    return undefined;
  }
  function getchildTable(item: SearchItem, cm: ChildModelAttribute, record: any) {
    item.childmodel = cm;
    item.parent = record;
    if (props.onSearchData != undefined) {
      props.onSearchData(item, (cmdata: TableData) => {
        const rows = [...data.rows];
        const index = rows.findIndex((r) => r.id == record.id)
        rows[index][cm.propertyname] = cmdata.rows;
        rows[index][cm.propertyname].total1 = cmdata.total;
        setData({ rows: rows, total: data.total });
      });
    }
  }
  const expandedRowTabPane = (childmodel, record) => {
    if (props.onExpandedRowTabPane) {
      const div = props.onExpandedRowTabPane(childmodel, record);
      if (div != undefined) return div;
    }
    return (
      <WayTable
        key={'id'}
        isselect={false}
        isedit={false}
        isclosecard={true}
        attr={childmodel}
        data={{
          rows: record[childmodel.propertyname],
          total: record[childmodel.propertyname].total1,
        }}
        onSearchData={(item: SearchItem) => {
          getchildTable(item, childmodel, record);
        }}
      />
    );
  };
  const expandedRowRender = (record: any) => {
    if (props?.attr?.childmodels != undefined && props.attr.childmodels.length > 0) {
      return (
        <Tabs
          defaultActiveKey={'0'}
          onChange={(activeKey) => {
            const cm = props.attr.childmodels[Number(activeKey)];
            getchildTable({}, cm, record);
          }}
        >
          {props.attr.childmodels.map((cm, index) => {
            if (!record[cm.propertyname]) record[cm.propertyname] = [];
            record[cm.propertyname].total = record[cm.propertyname].length;
            return (
              // eslint-disable-next-line react/no-array-index-key
              <TabPane tab={cm.title} key={index}>
                {expandedRowTabPane(cm, record)}
              </TabPane>
            );
          })}
        </Tabs>
      );
    }
  };
  const onRowSelect = (record, rowKey) => {
    try {
      if (rowSelection()?.type == 'radio') onSelectChange([record[rowKey]], [record]);
      if (rowSelection()?.type == 'checkbox') {
        const id = record[rowKey];
        let keys = [];
        if (selectedRowKeys.includes(id)) {
          keys = selectedRowKeys.filter((key) => key != id);
        } else {
          keys = [...selectedRowKeys];
          keys.push(id);
        }
        onSelectChange(keys, [record]);
      }
    } catch (error) {
      console.log(error);
    }
  }
  function renderTable() {
    const columns = getColumns(props, rowedit, editrow, setEditRow);
    const rowKey = 'id';
    return (
      <Table
        bordered={true}
        rowKey={rowKey}
        columns={columns}
        rowSelection={rowSelection()}
        scroll={{ x: columns.length * 150 }}
        dataSource={data.rows}
        pagination={{
          current: current,
          pageSize: currentSize,
          total: data.total,
          hideOnSinglePage: true,
          showTotal: (total) => `总 ${total} 行`,
        }}
        loading={loading}
        expandable={expandable()}
        onChange={handleTableChange}
        onRow={(record) => {
          return {
            onClick: (event) => {
              event.stopPropagation();
              try {
                if (rowSelection()?.type == 'radio') onSelectChange([record[rowKey]], [record]);
                if (rowSelection()?.type == 'checkbox') {
                  const id = record[rowKey];
                  let keys = [];
                  if (selectedRowKeys.includes(id)) {
                    keys = selectedRowKeys.filter((key) => key != id);
                  } else {
                    keys = [...selectedRowKeys];
                    keys.push(id);
                  }
                  onSelectChange(keys, [record]);
                }
              } finally {
                if (props.onRowClick) {
                  props.onRowClick(event, record);
                }
              }
            }, // 双点击行
            onDoubleClick: (event) => {
              event.stopPropagation();
              setrowedit(record);
              if (props.onRowDoubleClick) {
                props.onRowDoubleClick(event, record);
              }
            },
            onMouseEnter: (event) => {
              if (props.onRowMouseEnter) {
                props.onRowMouseEnter(event, record);
              }
            }, // 鼠标移入行
            onMouseLeave: (event) => {
              if (props.onMouseLeave) {
                props.onMouseLeave(event, record);
              }
            },
          };
        }}
      />
    );
  }
  function render() {
    //console.log('render');
    if (props.isclosecard) return renderTable();
    return <Card>{renderTable()}</Card>;
  }
  return render();
};

export default memo(WayTable);
