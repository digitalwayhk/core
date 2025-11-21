import React, { useState } from 'react';
import WayTextBox, { TextType } from '../WayTextBox';
import { Button, Input, Cascader } from 'antd';
import { SearchOutlined } from '@ant-design/icons';
import { SearchItem, SearchWhere, TableData, WayFieldAttribute } from '../way';
import { isArray } from 'lodash';
import { TypeDataToAny } from '../waymodel';

export interface WayProSearchProps {
  fields?: WayFieldAttribute[];
  onSearch: (w: SearchWhere) => void;
  onSearchData?: (item: SearchItem, callback: (data: TableData) => void) => void;
}
const dateItems = ['===', 'year', 'quarter', 'month', 'week'];
const WayProSearch: React.FC<WayProSearchProps> = (props) => {
  const symbolType = new Map<string, object[]>();
  symbolType.set('string', [
    { label: '等于', value: '=' },
    { label: '包含', value: 'like' },
    { label: '为空', value: 'isnull' },
    { label: '不为空', value: 'isnotnull' },
    { label: '左匹配', value: 'left' },
    { label: '右匹配', value: 'right' },
  ]);
  symbolType.set('int', [
    { label: '等于', value: '=' },
    { label: '大于', value: '>' },
    { label: '小于', value: '<' },
  ]);
  symbolType.set('datetime', [
    { label: '范围', value: 'date' },
    { label: '周', value: 'week' },
    { label: '月', value: 'month' },
    { label: '季', value: 'quarter' },
    { label: '年', value: 'year' },
  ]);

  const [text, setTextChange] = useState({ value: '', attr: { type: 'string' } });
  const [value, setValue] = useState<string>('');
  const [attr, setAttr] = useState<WayFieldAttribute>({ type: 'string' });
  const [nameType, setNameType] = useState({ name: '*', type: 'string', symbol: '' });
  const [textOption, setTextOption] = useState({
    picker: null,
  });
  const option = [{ label: '全部', value: '*' }];
  if (props.fields != undefined) {
    props.fields.forEach((item) => {
      if (item.issearch) {
        let child = item.searchsymbol ?? symbolType.get(item.type);
        if (item.type == 'datetime') {
          child = symbolType.get(item.type);
        }
        if (item.type == 'decimal') {
          child = symbolType.get('int');
        }
        if (item.foreign && item.foreign.isfkey) {
          child = undefined;
          // if (child) {
          //     var c = child.find((f => f.label == '等于'))
          //     child = [c]
          // }
        }
        option.push({ label: item.title, value: item.field, children: child });
      }
    });
  }
  function renderCascader() {
    return (
      <Cascader
        style={{ width: '140px' }}
        options={option}
        allowClear={false}
        defaultValue={['*']}
        expandTrigger={'hover'}
        onChange={(value) => {
          console.log(value);
          let name = value[0];
          const field = props.fields?.find((item) => item.field == name);
          if (field && field.porpfield) {
            name = field.porpfield;
          }
          setNameType({ name: name, symbol: value[1], type: field?.type });
          if (nameType.name != name) {
            setTextChange(Object.assign(text, { value: '', attr: field }));
            setValue('');
            setAttr(field);
            if (field?.type == 'datetime') {
              const data = { ...textOption };
              data.picker = value[1];
              setTextOption(data);
            }
          }
        }}
      />
    );
  }
  function renderTextBox() {
    return (
      <WayTextBox
        width={'240px'}
        options={textOption}
        search={true}
        name={nameType.name}
        attr={attr}
        value={value}
        onChange={(value) => {
          console.log(value);
          setValue(value);
          setTextChange({ value: value, attr: text.attr });
        }}
        onSearchBefore={(item, callback) => {
          if (props.onSearchData) {
            props.onSearchData(item, (data) => {
              callback(data.model, data);
            });
          }
        }}
        onSearchData={props.onSearchData}
      />
    );
  }
  function renderButton() {
    return (
      <Button
        style={{ width: '70px' }}
        type={'primary'}
        icon={<SearchOutlined />}
        onClick={() => {
          console.log(text);

          const svalue = TypeDataToAny(attr, text.value)
          console.log(svalue);
          const item = { name: nameType.name, symbol: nameType.symbol, value: svalue };
          if (item.name == '*' && item.value == '') props.onSearch();
          else {
            if (attr.type == 'datetime') {
              if (text.value != '' && isArray(svalue)) {
                const items = [];
                if (text.value[0] != null) {
                  item.symbol = '>=';
                  item.value = text.value[0];
                  items.push(item);
                }
                if (text.value[1] != null) {
                  items.push({
                    name: nameType.name,
                    symbol: '<=',
                    value: text.value[1],
                  });
                }
                return props.onSearch(items);
              }
            }
            // if (text.attr.type == 'boolean') {
            //   item.value = text.value ? '1' : '0';
            // }
            // if (text.attr.type != 'string') {
            //   item.value = text.value.toString();
            // }
            props.onSearch(item);
          }
        }}
      />
    );
  }
  function render() {
    return (
      <Input.Group compact>
        {renderCascader()}
        {renderTextBox()}
        {renderButton()}
      </Input.Group>
    );
  }
  return render();
};

export default WayProSearch;
