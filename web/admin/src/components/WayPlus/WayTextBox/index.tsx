import React, { useEffect, useState } from 'react';
import useMergeValue from 'use-merge-value';

import { DatePicker, Input, InputNumber, Select, Switch } from 'antd';
import dayjs from 'dayjs';
import { isArray, isMap, isNumber } from 'lodash';
import type { ModelAttribute, SearchItem, TableData, WayFieldAttribute } from '../way';
import WayEditTable from '../WayForm/edittable';
import { AnyToTypeData, DataToType, TypeDataToAny } from '../waymodel';

const { RangePicker } = DatePicker;
const { Search } = Input;

export enum TextType {
  Input = 'Input',
  TextArea = 'Input.TextArea',
  // eslint-disable-next-line @typescript-eslint/no-shadow
  InputNumber = 'InputNumber',
  Switch = 'Switch',
  DatePicker = 'DatePicker',
  RangePicker = 'RangePicker',
  Search = 'Input.Search',
  Avatar = 'Avatar',
  Select = 'Select',
}

export interface WayTextBoxProps {
  attr?: WayFieldAttribute;
  textType?: TextType;
  type?:
  | string
  | 'checkbox'
  | 'color'
  | 'date'
  | 'datetime'
  | 'email'
  | 'file'
  | 'hidden'
  | 'image'
  | 'month'
  | 'number'
  | 'password'
  | 'radio'
  | 'range'
  | 'tel'
  | 'text'
  | 'time'
  | 'url'
  | 'week';
  name?: string;
  search?: boolean;
  defaultValue?: string;
  value?: any;
  disabled?: boolean;
  options?: any;
  width?: string;
  onChange?: (value: any) => void;
  onPressEnter?: (value: any) => void;
  onSearchBefore?: (
    item: SearchItem,
    callback: (model: ModelAttribute, data: TableData) => void,
  ) => void;
  onSearchData?: (item: SearchItem, callback: (data: TableData) => void) => void;
  onSearchValueChange?: (obj: any) => void;
}
const gettextProps = (props: WayTextBoxProps) => {
  const prop = {
    //autoFocus: false,
    maxLength: 500,
    //prefix: '',
    size: 'middle',
    //suffix: '',
    //type: 'text',
    // allowClear: true,
    placeholder: '',
    disabled: props.disabled ?? false,
    style: { width: '100%' },
    texttype: TextType.Input,
  };
  for (const n in prop) {
    if (props.options != undefined && props.options[n] != undefined) {
      prop[n] = props.options[n];
    }
  }
  if (props.width != undefined) prop.style.width = props.width;
  // eslint-disable-next-line @typescript-eslint/no-use-before-define
  prop.texttype = getTextType(props, prop);
  return prop;
};
const getTextType = (props: WayTextBoxProps, prop: any): TextType => {
  // eslint-disable-next-line prefer-const
  let { textType, attr } = props;
  if (attr != undefined) {
    if (
      attr.type == 'int' ||
      attr.type == 'int32' ||
      attr.type == 'int64' ||
      attr.type == 'decimal'
    ) {
      textType = TextType.InputNumber;
      prop.min = attr.min;
      if (attr.precision != undefined && attr.precision > 0) {
        prop.precision = attr.precision;
      }
      prop.picker = undefined;
    }
    if (attr.type == 'datetime') {
      textType = TextType.DatePicker;
    }
    if (attr.type == 'boolean') {
      textType = TextType.Switch;
      if (props.search) {
        textType = TextType.Select;
        // eslint-disable-next-line no-var
        var items: { label: string; value: boolean }[] = [
          { label: '是', value: true },
          { label: '否', value: false },
        ];
        prop.options = items;
      }
    }
    if (attr.comvtp != undefined && attr.comvtp.isvtp) {
      textType = TextType.Select;
      const comitems: { label: string; value: number }[] = [];
      if (!isMap(attr.comvtp.items)) attr.comvtp.items = new Map(attr.comvtp.items);
      attr.comvtp.items.forEach((v, k) => {
        comitems.push({ label: v, value: k });
      });
      prop.options = comitems;
    }
    if (attr.foreign != undefined && attr.foreign.isfkey) {
      textType = TextType.Search;
    }
    if (attr.length != undefined && attr.length > 0) prop.maxLength = attr.length;
    if (props.disabled == undefined && attr.disabled != undefined) {
      prop.disabled = attr.disabled;
    }
    if (props.search) {
      prop.disabled = false;
    }
  }
  if (textType == undefined) {
    textType = TextType.Input;
  }
  return textType;
};
const WayTextBox: React.FC<WayTextBoxProps> = (props) => {
  const [defaultProps, setDefaultProps] = useState(() => {
    return gettextProps(props);
  });
  const [searchModal, setSearchModal] = useState({
    isshow: false,
    model: undefined,
    data: undefined,
  });
  const [searchValue, setSearchValue] = useState({
    row: undefined,
    value: '',
    text: '',
    rowfield: '',
  });
  const [value, setValue] = useMergeValue<any | undefined>(props.defaultValue, {
    value: anyToObject(props.value),
    onChange: (value: any, prevValue: any) => {
      console.log(value);
      if (props.onChange != undefined) {
        const vvv = anyToString(value);
        console.log(vvv);
        props.onChange(vvv);
      }
    },
  });
  useEffect(() => {
    //console.log(1);
    if (props.search) {
      if (searchValue.field != props.attr?.field) {
        setSearchRowToValue(null);
      }
      setDefaultProps(gettextProps(props));
    }
  }, [props.attr]);

  useEffect(() => {
    if (
      props.attr?.foreign != undefined &&
      props.attr.foreign.isfkey &&
      props.children != undefined
    ) {
      if (!props.search) {
        if (searchValue.row == undefined && props.value != undefined) {
          const row = props.children[props.attr.foreign.oneobjectfield];
          setSearchRowToValue(row);
        }
      }
    }
    //console.log(props.value);
  }, [props.value]);

  function anyToObject(value: any) {
    const { attr } = props;
    return AnyToTypeData(attr, value);
  }
  function anyToString(value: any) {
    const { attr } = props;
    switch (defaultProps.texttype) {
      case TextType.Select: {
        if (attr?.type == 'string') {
          return value.label;
        } else {
          return TypeDataToAny(attr, value);
        }
      }
    }
    return TypeDataToAny(attr, value);
  }
  function setSearchRowToValue(row: any) {
    const obj = { value: '', text: '', row: undefined, rowfield: '', field: '' };
    if (row) {
      const valueProp: string = props.attr?.foreign?.oneobjectfieldkey ?? '';
      const textProp: string = props.attr?.foreign?.onedisplayname ?? '';
      console.log(valueProp + '---' + textProp);
      obj.row = row;
      obj.value = row[valueProp];
      obj.text = row[textProp];
      obj.rowfield = props.attr?.foreign?.oneobjectfield ?? '';
      obj.field = props.attr?.field;
      console.log(obj);
    }
    setSearchValue(obj);
    setValue(obj.value);
    if (props.onSearchValueChange) props.onSearchValueChange(obj);
  }
  function renderSearch() {
    return (
      <>
        <Search
          {...defaultProps}
          size={'middle'}
          value={searchValue.text}
          onSearch={(value) => {
            if (props.onSearchBefore != undefined) {
              const item = { foreign: props.attr?.foreign, value: value, field: props.attr };
              props.onSearchBefore(item, (model, data) => {
                if (model.title == undefined || model.title == '') model.title = props.attr?.title;
                setSearchModal({
                  isshow: true,
                  model: model,
                  data: data,
                });
              });
            }
          }}
        />
        <WayEditTable
          ismodal={true}
          modelshow={searchModal.isshow}
          model={searchModal.model}
          data={searchModal.data}
          closeedit={true}
          commandShow={false}
          selectType={'radio'}
          closecard={true}
          onModalChange={(isshow: boolean, row: any) => {
            const sm = { ...searchModal };
            sm.isshow = isshow;
            console.log(sm);
            setSearchModal(sm);
            setSearchRowToValue(row);
          }}
          onSearchData={(item: SearchItem, callback) => {
            item.field = props.attr;
            item.foreign = props.attr?.foreign;
            if (props.onSearchData != undefined) {
              props.onSearchData(item, callback);
            }
          }}
        />
      </>
    );
  }
  function renderSelect() {
    const { attr } = props;
    if (attr?.type == 'string') {
      defaultProps.labelInValue = true;
    }
    return <Select {...defaultProps} size={'middle'} value={value} onChange={setValue} />;
  }
  function renderBool() {
    return <Switch size={'default'} checked={value} onChange={setValue} />;
  }
  function renderDate() {
    const { attr } = props;
    let showTime = undefined;
    let format = undefined;
    console.log(attr);
    if (attr && attr.datatimetype) {
      if (attr.datatimetype.istime) {
        showTime = { format: attr.datatimetype.timeformat };
      }
      format = attr.datatimetype.dateformat;
      if (attr.datatimetype.istime) {
        format = format + ' ' + attr.datatimetype.timeformat;
      }
    }
    if (props.search) {
      return (
        <RangePicker
          size={'middle'}
          showTime={showTime}
          format={format}
          style={defaultProps.style}
          value={value}
          allowEmpty={[true, true]}
          picker={props.options?.picker}
          onChange={setValue}
        />
      );
    } else {
      return (
        <DatePicker
          {...defaultProps}
          format={format}
          showTime={showTime}
          size={'middle'}
          picker={'date'}
          value={value}
          onChange={setValue}
        />
      );
    }
  }
  function renderNumber() {
    return <InputNumber {...defaultProps} value={value} onChange={setValue} />;
  }
  function render() {
    switch (defaultProps.texttype) {
      case TextType.InputNumber:
        return renderNumber();
      case TextType.DatePicker:
        return renderDate();
      case TextType.Switch:
        return renderBool();
      case TextType.Select:
        return renderSelect();
      case TextType.Search:
        return renderSearch();
    }
    return (
      <Input
        {...defaultProps}
        size={'middle'}
        value={value}
        onChange={(event: any) => {
          setValue(event.target.value);
        }}
      />
    );
  }
  return render();
};
export default WayTextBox;
