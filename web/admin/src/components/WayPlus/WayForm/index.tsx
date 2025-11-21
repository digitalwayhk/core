import React, { useEffect, useState, useCallback, useMemo, memo } from 'react';
import { Form, Row, Col, Card, Tabs } from 'antd';
import type {
  ChildModelAttribute,
  ModelAttribute,
  SearchItem,
  TableData,
  WayFieldAttribute,
} from '../way';
import type { WayTextBoxProps } from '../WayTextBox';
import WayTextBox from '../WayTextBox';
import type { FormItemProps, FormInstance } from 'antd/lib/form';
import WayEditTable from './edittable';
import DragModal from './window';
import { TypeConvert } from '../waymodel';

const TabPane = Tabs.TabPane;

export interface FormPlus extends FormInstance<any> {
  setFieldDisabled: (fieldName: string, disabled: boolean) => void;
  setTitle: (title: string) => void;
  setValues: (values: any) => void;
  show: () => void;
  clear: () => void;
  hide: () => void;
  setHideSearch: (isshow: boolean) => void;
  setHideToolbar: (isshow: boolean) => void;
  onFinish?: (values: any) => void;
}

interface WayFromProps {
  attr?: ModelAttribute;
  title?: string;
  ismodal?: boolean;
  modaltype?: string | 'modal' | 'drawer';
  isshow?: boolean;
  values?: any;
  onInitItem?: (field: WayFieldAttribute, item: FormItemProps) => void;
  onInitTextBox?: (field: WayFieldAttribute, txtprops: WayTextBoxProps) => void;
  onInitChildItems?: (model: ChildModelAttribute, item: FormItemProps) => void;
  onInitFormed?: (form: FormPlus) => void;
  onFinish?: (values: any) => void;
  onFieldRules?: (field: WayFieldAttribute, rules: any[]) => [];
  onSearchData?: (item: SearchItem, callback: (data: TableData) => void) => void;
}

const WayFrom: React.FC<WayFromProps> = (props) => {
  const [form] = Form.useForm();
  const [title, setTitle] = useState(props.title);
  const [isshow, setModalShow] = useState(props.isshow ?? false);
  const [formModel, setFormModel] = useState(filterModel(props.attr));
  const [values, setValues] = useState(() => setFormValues(props.values));
  const [closeToolbar, setCloseToolbar] = useState(false);
  const [closeSearch, setCloseSearch] = useState(false);
  useEffect(() => {
    console.log(props.values);
    if (props.values != undefined) {
      setValues(setFormValues(props.values));
    } else {
      clearFormValues();
    }
  }, [props.values]);
  useEffect(() => {
    setFormModel(filterModel(props.attr));
  }, [props.attr]);
  function filterModel(attr: ModelAttribute | undefined) {
    return {
      items: attr?.fields?.filter((field) => {
        return field.visible && field.isedit;
      }),
      models: attr?.childmodels?.filter((m) => {
        if (m.propertyname == undefined) {
          m.propertyname = m.name;
        }
        return m.visible;
      }),
    };
  }
  function setForm() {
    const children: JSX.Element[] = [];
    formModel.items?.forEach((field: WayFieldAttribute | undefined) => {
      const item: FormItemProps = fieldToItemProps(props.attr, field);
      //const txtprops: WayTextBoxProps = { options: { placeholder: item.label } };
      if (props.onInitTextBox != undefined) {
        props.onInitTextBox(field, txtprops);
      }
      children.push(
        <Col span={8}>
          <Form.Item key={field.field} {...item}>
            <WayTextBox
              // {...txtprops}
              attr={field}
              onSearchBefore={(item, callback) => {
                if (props.onSearchData != undefined) {
                  props.onSearchData(item, (data) => {
                    callback(data.model, data);
                  });
                }
              }}
              onSearchData={(item, callback) => {
                if (props.onSearchData) {
                  props.onSearchData(item, callback);
                }
              }}
            >
              {values}
            </WayTextBox>
          </Form.Item>
        </Col>,
      );
    });
    if (props.onInitFormed != undefined) {
      form.setFieldDisabled = (fieldName: string, disabled: boolean) => {
        formModel.items?.forEach((item) => {
          if (item.field?.toLocaleLowerCase() == fieldName.toLocaleLowerCase())
            item.disabled = disabled;
        });
        setFormModel(formModel);
      };
      form.setTitle = (title: string) => {
        setTitle(title);
      };
      form.show = () => {
        setModalShow(true);
      };
      form.hide = () => {
        setModalShow(false);
      };
      form.setValues = (values: any) => {
        setValues(setFormValues(values));
      };
      form.clear = () => {
        clearFormValues();
      };
      form.setHideSearch = (isshow: Boolean) => {
        setCloseSearch(isshow);
      };
      form.setHideToolbar = (isshow: Boolean) => {
        setCloseToolbar(isshow);
      };
      props.onInitFormed(form);
    }
    return children;
  }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  function setFormValues(values: any) {
    if (values == undefined) values = {};
    form.setFieldsValue(values);
    if (formModel.models != undefined && formModel.models?.length > 0) {
      console.log(formModel);
      formModel.models.forEach((cm) => {
        if (!values[cm.propertyname]) {
          values[cm.propertyname] = [];
          values[cm.propertyname].total = 0;
        } else {
          values[cm.propertyname].total = values[cm.propertyname].length;
        }
        cm.removeRows = [];
      });
    }
    return values;
  }

  // eslint-disable-next-line react-hooks/exhaustive-deps
  function clearFormValues() {
    form.resetFields();
    const old = {};
    formModel.models?.forEach((cm: any) => {
      if (!old[cm.propertyname]) {
        old[cm.propertyname] = [];
        old[cm.propertyname].total = 0;
      }
    });
    setValues(old);
  }
  function fieldToItemProps(model: ModelAttribute, field: WayFieldAttribute): FormItemProps {
    const item: FormItemProps = {};
    //item.key = field.field;
    item.name = field.field;
    item.label = field.title;
    const rules = fieldGetRules(field);
    if (!field.disabled) item.rules = rules;
    if (props.onInitItem != undefined) {
      props.onInitItem(field, item);
    }
    return item;
  }
  function fieldGetRules(field: WayFieldAttribute) {
    let rules = [];
    if (field.required) rules.push({ required: field.required });
    if (field.length ?? 0 > 0) {
      rules.push({ max: field.length ?? 500, type: TypeConvert(field.type) });
    }
    if (props.onFieldRules != undefined) rules = props.onFieldRules(field, rules);
    return rules;
  }
  const getFormValue = (finishValue: any) => {
    const res = Object.assign({}, values);
    for (const n in finishValue) res[n] = finishValue[n];
    if (formModel.models != undefined && formModel.models?.length > 0) {
      formModel.models.forEach((cm) => {
        if (res[cm.propertyname] != undefined) {
          res[cm.propertyname].forEach((row) => {
            if (row.isnew) {
              row.modelState = 1
            } else {
              row.modelState = 2
            }
          })
        }
        cm.removeRows?.forEach((row) => {
          row.modelState = 3;
          res[cm.propertyname].push(row);
        });
      });
    }
    console.log(res);
    return res;
  };
  function renderForm() {
    return (
      <Form
        labelAlign="left"
        form={form}
        onFinish={(formvalues) => {
          const res = getFormValue(formvalues);
          if (form.onFinish != undefined) {
            form.onFinish(res);
          }
          if (props.onFinish != undefined) {
            props.onFinish(res);
          }
        }}
        scrollToFirstError={true}
        initialValues={props.values}
      >
        <Row gutter={24}>{setForm()}</Row>
      </Form>
    );
  }
  function renderChildTables() {
    if (formModel.models != undefined && formModel.models?.length > 0) {
      return (
        <Tabs defaultActiveKey={'0'}>
          {formModel.models?.map((cm, index) => {
            return (
              // eslint-disable-next-line react/no-array-index-key
              <TabPane tab={cm.title} key={index}>
                <WayEditTable
                  model={cm}
                  data={{ rows: values[cm.propertyname], total: values[cm.propertyname]?.total }}
                  iscirclebutton={true}
                  closetoolbar={closeToolbar}
                  closesearch={closeSearch}
                  closecard={true}
                  onSearchData={(item) => {
                    if (props.onSearchData != undefined) {
                      item.parent = values;
                      item.childmodel = cm;
                      props.onSearchData(item, (data: TableData) => {
                        const row = Object.assign({}, values);
                        row[cm.propertyname] = data.rows;
                        row[cm.propertyname].total = data.total;
                        setValues(row);
                      });
                    }
                  }}
                  onDataChange={(data, row, type) => {
                    console.log(data, row);
                    const vvv = Object.assign({}, values);
                    vvv[cm.propertyname] = data.rows;
                    vvv[cm.propertyname].total = data.total;
                    console.log(vvv);
                    if (type == 'remove') {
                      row.forEach((r) => {
                        if (r && !r.isnew) cm.removeRows.push(r);
                      });
                    }
                    setValues(vvv);
                  }}
                />
              </TabPane>
            );
          })}
        </Tabs>
      );
    }
    return <></>;
  }
  function render() {
    if (props.ismodal) {
      return (
        <DragModal
          title={title}
          width={1100}
          visible={isshow}
          onCancel={() => setModalShow(false)}
          onOk={() => {
            form.submit();
          }}
        >
          <Card>{renderForm()}</Card>
          {renderChildTables()}
        </DragModal>
      );
    } else {
      return (
        <>
          <Card title={title}>{renderForm()}</Card>
          {renderChildTables()}
        </>
      );
    }
  }
  return render();
};

export default memo(WayFrom);
