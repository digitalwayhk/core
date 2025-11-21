export interface WayFieldAttribute {
  iskey: boolean | false;
  field: string;
  porpfield?: string; // 属性字段名
  title?: string;
  index?: number;
  disabled?: boolean;
  visible?: boolean;
  isedit?: boolean;
  type?: string;
  required?: boolean;
  length?: number;
  issearch?: boolean; //是否可作为查询条件
  isremark?: boolean;
  ispassword?: boolean; //是否不显示
  min?: number; //最小值，当type为数字时有效
  max?: number; //最大值，当type为数字时有效
  precision?: number; //数值精度，当type为数字时有效
  tag?: any;
  comvtp?: ComboxAttribute;
  foreign?: ForeignAttribute;
  sorter?: boolean;
  datatimetype?: DataTimeType | null;
  posttype?: string;
}
export interface DataTimeType {
  isdata: boolean;
  istime: boolean;
  dateformat: string;
  timeformat: string;
  isutc: boolean;
}
export interface ComboxAttribute {
  isvtp: boolean;
  multiple?: boolean;
  items: Map<number, string>;
  eventrow?: any;
}
export interface ForeignAttribute {
  isfkey: boolean; // 是否外键
  oneobjecttypename: string; // 主对象类型全名程
  oneobjectname: string; // 主对象类型名称
  oneobjectfield: string; //子对象中的主对象属性名称
  oneobjectfieldkey: string; // 关联到主对象中的属性名程【默认为ID】
  oneObjectforeignkeyvalue: string; // 主对象可用的外建值
  manyobjecttypename: string; // 子对象类型全名
  manyobjectname: string; // 子对象的类型名
  manyobjectfield: string; // 子对象的属性名
  manyobjectfiledkey: string; // 子对象中用于关联外键的属性名
  onedisplayname: string; // 主对象中需显示的扩展属性名称
  manydisplayfield: string; // 子对象中扩展显示的属性名，通常为阴影字段名
  mapitems: Map<string, string>; // 主外键对象对应关系，keys中one对象属性名称、Values中为many对象属性名称
  eventrow: any; // 激活外键的主数据行
  isassociate: boolean; // 是否子关联外键
  model?: ModelAttribute;
  data?: TableData;
}
export interface ModelAttribute {
  name?: string;
  title?: string;
  servicename?: string;
  disabled?: boolean;
  visible?: boolean;
  autoload?: boolean;
  fields?: WayFieldAttribute[];
  childmodels?: ChildModelAttribute[];
  commands?: CommandAttribute[];
  viewtype?: string;
}
export interface CommandAttribute {
  command: string;
  name: string;
  title?: string;
  isselectrow?: boolean;
  selectmultiple?: boolean;
  isalert?: boolean;
  editshow?: boolean;
  issplit?: boolean;
  splitname?: string;
  disabled?: boolean;
  visible?: boolean;
  onclick?: string;
  icon?: string;
  hander?: () => void;
}
export interface ChildModelAttribute extends ModelAttribute {
  isadd?: boolean;
  isedit?: boolean;
  isremove?: boolean;
  isselect?: boolean;
  ischeck?: boolean;
  foreignKey?: string;
  propertyname?: string;
}
export interface SearchWhere {
  name: string;
  symbol: string;
  value: string;
}
export interface SearchItem {
  field?: WayFieldAttribute;
  foreign?: ForeignAttribute;
  parent?: any;
  childmodel?: ChildModelAttribute;
  page: number;
  size: number;
  whereList?: SearchWhere[];
  sortList?: string[];
  value?: any;
}
export interface TableData {
  rows: any[];
  total: number;
}
export interface ResultData {
  success: boolean;
  code: number;
  message: string;
  data: TableData | object;
  showtype: number;
  traceid: string;
  host: string;
}
export interface DefaultModelState {
  model: ModelAttribute | null;
  result: ResultData | null;
}

// Redux types
export type Effect = (action: any, effects: any) => Generator<any, any, any>;
export type Reducer<S = any> = (state: S, action: any) => S;

export interface DefaultModelType {
  namespace: string;
  state: DefaultModelState;
  effects: {
    init: Effect;
    search: Effect;
    execute: Effect;
  };
  reducers: {
    inited: Reducer<DefaultModelState>;
    searched: Reducer<DefaultModelState>;
    executed: Reducer<DefaultModelState>;
  };
}

// 通用数据类型
export type RecordData = Record<string, any>;

// 分页查询响应
export interface PageResponse<T = any> {
  rows: T[];
  total: number;
  page?: number;
  size?: number;
}
