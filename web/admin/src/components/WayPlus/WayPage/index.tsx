import React, { useEffect, useState } from 'react';
import { Col, message, Row } from 'antd';
import WayToolbar from '../WayToolbar';
import WayTable from '../WayTable';
import type { FormPlus } from '../WayForm';
import WayForm from '../WayForm';
import type {
  ChildModelAttribute,
  CommandAttribute,
  ModelAttribute,
  SearchItem,
  SearchWhere,
  TableData,
} from '../way';
import { PageContainer } from '@ant-design/pro-components';
import { isArray } from 'lodash';
import ImportForm from '../WayTable/importform';
import { pageExportExcel } from '../WayTable/exportform';

interface WayPageProps {
  namespace?: string;
  controller: string;
  title?: string;
  service: string;
  onCommandClick?: (command: string) => void;
  onExpandedRowTabPane?: (childmodel: ChildModelAttribute, record: any) => React.ReactElement;
  // Redux 注入的方法
  init?: () => Promise<any>;
  search?: (item: SearchItem) => Promise<any>;
  execute?: (command: string, item: any) => Promise<any>;
}
const WayPage: React.FC<WayPageProps> = (props) => {
  const [loading, setLoading] = useState(false);
  const [values, setValues] = useState<any>(null);
  const [selectCount, setSelectCount] = useState(0);
  const [keys, setKeys] = useState<any[]>([]);
  const [model, setModel] = useState<ModelAttribute | undefined>(undefined);
  const [data, setData] = useState<TableData>({ rows: [], total: 0 });
  const [importShow, setImportShow] = useState(false);
  const [form, setForm] = useState<FormPlus | null>(null);
  const [searchItem, setSearchItem] = useState<SearchItem>({
    page: 1,
    size: 10,
    whereList: [] as SearchWhere[],
    sortList: [] as string[],
  });
  const [current, setCurrent] = useState(1);
  const [initError, setInitError] = useState<string | null>(null);
  useEffect(() => {
    console.log('🔄 WayPage useEffect 触发', { controller: props.controller, namespace: props.namespace, service: props.service });
    setModel(undefined);
    setValues(null);
    setSearchItem({
      page: 1,
      size: 10,
      whereList: [],
      sortList: [],
    })
    setSelectCount(0);
    setKeys([]);
    setData({ rows: [], total: 0 });
    setInitError(null);
    view();
  }, [props.controller]);

  function view() {
    console.log('🚀 waypage.init 开始');
    console.log('📦 props:', props);

    if (!props.init) {
      const modelname = props.namespace ?? props.controller;
      const errorMsg = modelname + ' model 未创建或init方法未实现，不能初始化page.';
      console.error('❌', errorMsg);
      setInitError(errorMsg);
      message.error(errorMsg);
      return;
    }

    setLoading(true);
    props.init().then((result: any) => {
      console.log('✅ init 返回结果:', result);
      if (result == undefined) {
        setLoading(false);
        setInitError('初始化失败：返回结果为空');
        return;
      }
      if (result.success) {
        setModel(result.data);
        setLoading(false);
        console.log('✅ Model 设置成功:', result.data);
        if (result.data.autoload) {
          const initSearchItem: SearchItem = {
            page: 1,
            size: 10,
            whereList: [],
            sortList: [],
          };
          searchDataThan(initSearchItem, (data: TableData) => {
            setData(data);
          });
        }
      } else {
        setLoading(false);
        const errorMsg = result.message || '初始化失败';
        setInitError(errorMsg);
        resultMessage(result.message);
      }
    }).catch((error: any) => {
      console.error('❌ init 错误:', error);
      setLoading(false);
      const errorMsg = '初始化失败: ' + (error.message || '未知错误');
      setInitError(errorMsg);
      resultMessage(error);
    });
  }

  function searchDataThan(item: SearchItem, callback?: (data: TableData) => void) {
    if (!props.search) {
      console.error('❌ search 方法未定义');
      return;
    }
    setSelectCount(0);
    setLoading(true);
    props.search(item).then((result: any) => {
      if (result == null) return;
      setLoading(false);
      if (result != undefined && result.success) {
        if (result.data == undefined) {
          result.data = { rows: [], total: 0 };
        }
        if (result.data.rows == null) result.data.rows = [];
        if (item.foreign == undefined && item.childmodel == undefined) {
          if (keys.length > 0) {
            const row = result.data.rows.find((row: any) => {
              return row["id"] == keys[0]
            })
            if (row != undefined) {
              setKeys(keys)
              setSelectCount(keys.length);
              setValues(row);
            } else {
              setKeys([])
              setSelectCount(0);
              setValues(null);
            }
          }
        }
        if (callback) callback(result.data);
        else {
          setData(result.data);
        }
      } else {
        resultMessage(result.message);
      }
    }, (error) => {
      console.log(error);
      setLoading(false);
    });
  }

  const executeCommand = (command: CommandAttribute) => {
    let item = null;
    if (command.isselectrow) item = values;
    if (command.selectmultiple) item = keys;
    // eslint-disable-next-line @typescript-eslint/no-use-before-define
    executeCommandData(command, item);
  };
  const executeCommandData = (command: CommandAttribute, values: any) => {
    if (!props.execute) {
      console.error('❌ execute 方法未定义');
      message.error('执行失败：方法未定义');
      return;
    }
    setLoading(true);
    props.execute(command.command, values).then((result: any) => {
      console.log(result);
      if (result != undefined && result.success) {
        setLoading(false);
        message.success(command.name + '完成');
        if (model?.viewtype != 'form') {
          if (form) {
            form.hide();
          }
          searchDataThan(searchItem, (data) => {
            setData(data);
          });
        }
      } else {
        setLoading(false);
        resultMessage(result.message);
      }
    }, (error) => {
      console.log(error);
      setLoading(false);
    });
  };
  function isObject(obj: unknown): obj is Record<string, any> {
    return typeof obj === 'object' && obj !== null;
  }

  function resultMessage(error: any) {
    console.error('Error:', error);
    let msg = error?.data?.errorMessage;
    if (msg == undefined) {
      if (isObject(error) && error.message) {
        msg = error.message;
      }
      if (error?.response?.errorMessage) {
        msg = error.response.errorMessage;
      }
      if (error?.response?.message) {
        msg = error.response.message;
      }
    }
    // 使用 notification 替代 Modal.error，提供更好的用户体验
    message.error(msg || '操作失败，请稍后重试');
  }
  function fromshow(command: CommandAttribute, row: any) {
    if (!form) {
      console.error('❌ form 未初始化');
      message.error('表单未初始化');
      return;
    }
    form.clear();
    form.setHideSearch(true);
    form.setTitle(model?.title + '-' + command.name);
    form.show();
    if (row) {
      form.setValues(row);
      form.setHideSearch(false);
    }
    // eslint-disable-next-line @typescript-eslint/no-shadow
    form.onFinish = (values: any) => {
      console.log(values);
      executeCommandData(command, values);
    };
  }
  function renderToolbar(issearch: boolean) {
    // 添加调试信息
    console.log('renderToolbar - model:', model);
    console.log('renderToolbar - data:', data);
    console.log('renderToolbar - selectCount:', selectCount);
    console.log('renderToolbar - values:', values);
    const serach = issearch
      ? {
        fields: model?.fields?.filter((f) => f.issearch ?? true),
        onSearch: (w: SearchWhere) => {
          setLoading(true);
          const item: SearchItem = {
            page: 1,
            size: 10,
            whereList: [] as SearchWhere[],
            sortList: [] as string[],
          };
          if (w != undefined) {
            if (isArray(w)) {
              item.whereList = w as SearchWhere[];
            } else {
              item.whereList = [w];
            }
          }
          setSearchItem(item);
          setCurrent(1);
          searchDataThan(item, (data) => {
            setData(data);
          });
        },
        onSearchData: searchDataThan,
      }
      : false;
    try {
      return (
        <WayToolbar
          attrs={model?.commands}
          isselectrow={true}
          selectcount={selectCount}
          commandShow={true}
          // helpShow={{ ishelp: true }} // isprint: true, isset: true,
          onClick={(name: string, command: CommandAttribute) => {
            console.log(name);
            if (name == 'ImportData' || name == 'importdata') {
              setImportShow(true);
              return;
            }
            if (name == 'ExportData' || name == 'exportdata') {
              pageExportExcel(model, data.total, searchItem, props.search, props.title + '.xlsx');
              return;
            }
            if (name == 'add') {
              if (model?.viewtype == 'form') {
                if (form) {
                  form.onFinish = (values: any) => {
                    console.log(values);
                    executeCommandData(command, values);
                  };
                  form.submit();
                }
                return;
              }
              fromshow(command, null);
              return;
            }
            if (name == 'edit') {
              fromshow(command, values);
              return;
            }
            executeCommand(command);
          }}
          searchShow={serach}
        />
      );
    } catch (error) {
      console.error('renderToolbar error:', error);
      return <div>工具栏渲染错误</div>;
    }
  }
  function renderTable() {
    return (
      <WayTable
        attr={model}
        data={data}
        isselect={true}
        isexpandable={true}
        loading={loading}
        current={current}
        onSelectRows={(row, keys) => {
          console.log(row);
          console.log(keys);
          setKeys(keys);
          setSelectCount(keys.length);
          setValues(row);
        }}
        onSearchData={(item, callback) => {
          if (item.parent && item.childmodel) {
            //子表查询
            searchDataThan(item, (data) => {
              callback(data);
            });
            return;
          }
          setLoading(true);
          setCurrent(item.page);
          item.whereList = searchItem.whereList;
          setSearchItem(item);
          searchDataThan(item, (data) => {
            setData(data);
          });
        }}
        onExpandedRowTabPane={props.onExpandedRowTabPane}
        onRowDoubleClick={(event, record) => {
          // eslint-disable-next-line @typescript-eslint/no-shadow
          const cmd = model?.commands?.find((cmd) => {
            return cmd.command == 'edit' && cmd.visible;
          });
          if (cmd != undefined) {
            fromshow(cmd, record);
          }
        }}
      />
    );
  }
  function renderForm(ismodel: boolean) {
    return (
      <WayForm
        attr={model}
        title={props.title}
        ismodal={ismodel}
        onInitFormed={(f) => {
          setForm(f);
        }}
        onSearchData={searchDataThan}
      />
    );
  }
  function render() {
    // 显示错误信息
    if (initError) {
      return (
        <PageContainer title={props.title || '页面加载失败'}>
          <div style={{ padding: 24, backgroundColor: '#fff2f0', border: '1px solid #ffccc7', borderRadius: 4 }}>
            <h3 style={{ color: '#ff4d4f' }}>⚠️ 初始化失败</h3>
            <p>{initError}</p>
            <pre style={{ backgroundColor: '#f5f5f5', padding: 12, borderRadius: 4, marginTop: 12 }}>
              {JSON.stringify({
                controller: props.controller,
                namespace: props.namespace,
                service: props.service,
              }, null, 2)}
            </pre>
          </div>
        </PageContainer>
      );
    }

    // 显示加载状态
    if (!model && loading) {
      return (
        <PageContainer title={props.title || '加载中...'}>
          <div style={{ padding: 48, textAlign: 'center' }}>
            <p>正在加载...</p>
          </div>
        </PageContainer>
      );
    }

    // model 还未加载完成
    if (!model) {
      return (
        <PageContainer title={props.title || '准备中...'}>
          <div style={{ padding: 48, textAlign: 'center' }}>
            <p>正在初始化...</p>
          </div>
        </PageContainer>
      );
    }

    if (model?.viewtype == 'form') {
      return renderViewForm();
    }
    return (
      <PageContainer title={props.title}>
        <Row gutter={[16, 16]}>
          <Col span={24}>{renderToolbar(true)}</Col>
        </Row>
        <Row gutter={[16, 16]}>
          <Col span={24}>{renderTable()}</Col>
        </Row>
        <Row gutter={[16, 16]}>
          <Col span={24}>{renderForm(true)}</Col>
        </Row>
        {model && (
          <ImportForm
            title={props.title}
            isShow={importShow}
            attr={model}
            onAdd={props.execute}
            form={form || undefined}
            onShowChange={(show) => {
              setImportShow(show);
              if (!show) {
                searchDataThan(searchItem, (data) => setData(data));
              }
            }}
            onSearchData={searchDataThan}
          />
        )}
      </PageContainer>
    );
  }
  function renderViewForm() {
    return (
      <PageContainer title={props.title}>
        <Row gutter={[16, 16]}>
          <Col span={24}>{renderToolbar(false)}</Col>
        </Row>
        <Row gutter={[16, 16]}>
          <Col span={24}>{renderForm(false)}</Col>
        </Row>
      </PageContainer>
    );
  }
  return render();
};

export default WayPage;
