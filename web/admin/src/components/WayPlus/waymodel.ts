import { init, search, execute } from './request';
import { isArray, isNumber } from 'lodash';
import dayjs from 'dayjs';
import type {
  CommandAttribute,
  ModelAttribute,
  ResultData,
  WayFieldAttribute,
  ChildModelAttribute,
} from './way';

interface WayModelProps {
  /**开始初始化--可以在args中增加stop=true停止服务端请求*/
  initing?: (args: any) => any;
  /**初始化完成数据获取 获取到初始化数据result*/
  inited?: (args: any, result: ResultData) => ResultData;
  /**开始查询,model中的所有查询,args.c为控制器名称，args.item为查询条件--可以在args中增加stop=true停止服务端请求*/
  searching?: (args: any) => any;
  /**查询完成,获取到的查询结果result*/
  searched?: (args: any, result: ResultData) => ResultData;
  /**开始执行,所有命令执行,args.c为控制器名称，args.m为命令名称，args.item为提交参数--可以在args中增加stop=true停止服务端请求*/
  execing?: (args: any) => any;
  /**执行完成,执行结果result*/
  execed?: (args: any, result: ResultData) => ResultData;
}
export const CreateCommand = (cmd: string, name: string): CommandAttribute => {
  return {
    command: cmd,
    name: name,
    title: name,
    isselectrow: false,
    selectmultiple: false,
    isalert: false,
    editshow: false,
    issplit: false,
    splitname: '',
    disabled: true,
    visible: true,
    onclick: '',
    icon: '',
    hander: undefined,
  };
};
export const CreateField = (field: string, porpfield: string, title: string): WayFieldAttribute => {
  console.log('CreateField', field, porpfield, title);
  return {
    iskey: false,
    field: field,
    porpfield: porpfield,
    title: title,
    index: 1,
    disabled: false,
    visible: true,
    isedit: true,
    type: 'string',
    required: false,
    length: 500,
    precision: 0,
    issearch: true,
    isremark: false,
    sorter: true,
    tag: null,
    datatimetype: null,
    posttype: '',
    comvtp: {
      isvtp: false,
      multiple: false,
      items: undefined,
      eventrow: null,
    },
    foreign: {
      isfkey: false,
      oneobjecttypename: '',
      oneobjectname: '',
      oneobjectfield: '',
      oneobjectfieldkey: '',
      oneObjectforeignkeyvalue: '',
      manyobjecttypename: '',
      manyobjectname: '',
      manyobjectfiled: '',
      manyobjectfiledkey: '',
      onedisplayname: '',
      manydisplayfield: '',
      mapitems: null,
      eventrow: null,
      isassociate: false,
      model: undefined,
      data: undefined,
    },
  };
};
export const getAdd = (): CommandAttribute => {
  return CreateCommand('add', '新增');
};
export const getEdit = (): CommandAttribute => {
  const edit = CreateCommand('edit', '编辑');
  edit.isselectrow = true;
  return edit;
};
export const getRomove = (): CommandAttribute => {
  const remove = CreateCommand('remove', '删除');
  remove.isselectrow = true;
  remove.selectmultiple = true;
  remove.isalert = true;
  return remove;
};
export const ToField = (item: WayFieldAttribute): WayFieldAttribute => {
  const field = CreateField(item.field, item.porpfield, item.title ?? '');
  if (item.comvtp) {
    for (const n in field.comvtp) {
      if (n == 'items') {
        if (item.comvtp.items) {
          const nsmap = new Map<number, string>();
          for (const i in item.comvtp.items) {
            nsmap.set(Number(i), item.comvtp.items[i]);
            field.comvtp[n] = nsmap;
          }
        }
      } else {
        field.comvtp[n] = item.comvtp[n];
      }
    }
  }
  if (item.foreign) {
    for (const n in field.foreign) {
      field.foreign[n] = item.foreign[n];
    }
  }
  for (const n in field) {
    if (n == 'comvtp') continue;
    if (n == 'foreign') continue;
    if (item[n] != undefined) field[n] = item[n];
  }
  return field;
};
export const ToCommand = (item: CommandAttribute): CommandAttribute => {
  const cmd = CreateCommand(item.command, item.name);
  for (const n in cmd) {
    if (item[n] != undefined) cmd[n] = item[n];
  }
  return cmd;
};

function compare(p: any) {
  return function (m: any, n: any) {
    const a: any = m[p];
    const b: any = n[p];
    return a - b; //升序
  };
}
const initmodel = (model: ModelAttribute | ChildModelAttribute) => {
  if (model.commands) {
    model.commands.forEach((cmd, index) => {
      // eslint-disable-next-line no-param-reassign
      model.commands[index] = ToCommand(cmd);
    });
  }
  if (model.fields) {
    model.fields.sort(compare('index'));
    model.fields.forEach((field, index) => {
      // eslint-disable-next-line no-param-reassign
      const item = ToField(field);
      if (item.type == 'bool') {
        item.type = 'boolean';
      }
      if (item.type == 'date') {
        item.type = 'datatime';
      }
      if (item.type == 'uint' || item.type == 'bigint') {
        item.type = 'int';
      }
      model.fields[index] = item;
    });
  }
  if (model.childmodels) {
    model.childmodels.forEach((cm) => {
      if (cm.propertyname == undefined) {
        cm.propertyname = cm.name;
      }
    });
  }
};

export const WayModel = (props?: WayModelProps) => {
  return {
    state: {
      model: null,
      result: null,
    },
    effects: {
      *init(args: any, { call, put }) {
        console.log('\ud83d\ude80 [waymodel.ts] init effect 被触发:', args);
        let result;
        if (props?.initing) {
          console.log('\ud83d\udd27 [waymodel.ts] 执行 initing 钩子');
          result = props?.initing(args);
        }
        console.log('\ud83d\udcca [waymodel.ts] args.payload:', args.payload);
        if (!args.stop) {
          console.log('\ud83d\udce6 [waymodel.ts] 准备调用 request.init...');
          result = yield call(async () => await init(args.payload));
          console.log('\u2705 [waymodel.ts] request.init 返回:', result);
          if (result.success && result.data) {
            console.log('\ud83c\udf89 [waymodel.ts] 初始化成功, 处理 model 数据:', result.data);
            initmodel(result.data);
            if (result.data.childmodels && result.data.childmodels.length > 0) {
              for (let i = 0; i < result.data.childmodels.length; i++) {
                initmodel(result.data.childmodels[i]);
              }
            }
          }
        }
        console.log(result);
        if (props?.inited) {
          result = props.inited(args, result);
        }
        yield put({ type: 'inited', value: result });
        return result;
      },
      *search(args, { call, put }) {
        console.log(args);
        let result;
        if (props?.searching) result = props?.searching(args);
        if (!args.stop) {
          result = yield call(async () => await search(args.payload));
        }
        if (props?.searched) result = props?.searched(args, result);
        const item = args.payload.item;
        if (item.field && item.foreign) {
          console.log(result);
          //外键查询
          return result;
        }
        if (item.parent && item.childmodel) {
          console.log(result);
          //子集查询
          return result;
        }
        console.log(result);
        // yield put({ type: 'searched', value: result });
        return result;
      },
      *execute(args, { call, put }) {
        console.log(args);
        let result;
        if (props?.execing) result = props?.execing(args);
        args.payload.m = args.payload.command;
        if (!args.stop) {
          result = yield call(async () => await execute(args.payload));
        }
        if (props?.execed) result = props?.execed(args, result);
        return result;
        //yield put({ type: 'executed', value: result });
      },
    },
    reducers: {
      inited(state: any, action: { value: any }) {
        const obj = { ...state };
        obj.model = action.value;
        return obj;
      },
      searched(state: any, action: { value: any }) {
        const obj = { ...state };
        obj.result = action.value;
        return obj;
      },
      executed(state: any, action: { value: any }) {
        const obj = { ...state };
        obj.result = action.value;
        return obj;
      },
    },
  };
};

export const TypeConvert: any = (type: string) => {
  const numberkeys = [
    'int',
    'int32',
    'int64',
    'float',
    'decimal',
    'double',
    'bigint',
    'uint',
    'uint32',
    'uint64',
    'uint128',
  ];
  // const datekeys = ['datetime', 'date', 'time', 'float', 'decimal'];
  if (numberkeys.includes(type)) {
    return 'number';
  }
  return 'string';
};
export const DataToPostType: any = (attr: WayFieldAttribute, value: any) => {
  if (attr.posttype == 'string') {
    return value.toString();
  }
  return value;
};
export const AnyToTypeData: any = (attr: WayFieldAttribute, value: any) => {
  if (value == undefined || value == null || attr == undefined) {
    return value;
  }
  if (TypeConvert(attr.type) == 'number') {
    return Number(value);
  }
  if (attr.type == 'date' || attr.type == 'datetime' || attr.type == 'time') {
    if (typeof value == 'string') {
      if (value == '') return null;
      console.log(value);
      return dayjs(value)
    } else {
      if (!isArray(value)) return dayjs(value);
    }
  }
  return value;
};
const getTimeData: any = (attr: WayFieldAttribute, value: any) => {
  if (value.format) {
    //const fdata = attr.datatimetype?.dateformat + " " + attr.datatimetype?.timeformat;
    const data = dayjs(value).format('YYYY-MM-DDTHH:mm:ss.SSSZ')
    const str = data.toString();
    return str
  }
  return value.toString();
}
export const TypeDataToAny: any = (attr: WayFieldAttribute, value: any) => {
  if (value == undefined || value == null || attr == undefined) {
    return value;
  }
  if (attr.type == 'date' || attr.type == 'datetime' || attr.type == 'time') {
    console.log(value);
    if (value != null || value != undefined) {
      if (!isArray(value)) {
        return getTimeData(attr, value);
      } else {
        value[0] = getTimeData(attr, value[0]);
        value[1] = getTimeData(attr, value[1]);
      }
    }
  }
  if (TypeConvert(attr.type) == 'number' && attr.posttype == '') {
    return Number(value);
  }
  if (attr.posttype == 'string') {
    return value.toString();
  }
  return value;
};
