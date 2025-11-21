import React, { useState, useCallback, useMemo, memo } from 'react';
import { Row, Col, Space, Menu, Divider, Card } from 'antd';
import Icon, {
  DownOutlined,
  PlusOutlined,
  EditOutlined,
  DeleteOutlined,
  SyncOutlined,
  PrinterOutlined,
  QuestionOutlined,
  InfoOutlined,
  SettingOutlined,
  ExclamationCircleOutlined,
  SearchOutlined,
  RollbackOutlined,
  ClearOutlined,
  SaveOutlined,
  FastForwardFilled,
  WhatsAppOutlined,
  ImportOutlined,
  ExportOutlined,
} from '@ant-design/icons';
import WayButton from './waybutton';
import type { WayProSearchProps } from './wayprosearch';
import WayProSearch from './wayprosearch';
import type { CommandAttribute } from '../way';

export interface WayToolbarProps {
  attrs?: CommandAttribute[];
  searchShow?: false | WayProSearchProps;
  commandShow?: boolean;
  helpShow?: false | WayHelpProps;
  viewOredit?: string | 'view' | 'edit';
  iscircle?: boolean;
  isselectrow?: boolean;
  selectcount?: number;
  isclosecard?: boolean;
  onClick?: (name: string, command: CommandAttribute) => void;
  onCommandDisabled?: (command: CommandAttribute) => boolean;
}
interface WayHelpProps {
  isprint?: boolean;
  isset?: boolean;
  ishelp?: boolean;
  isabout?: boolean;
  wvh?: DefultViewHelp;
}
class DefultViewHelp {
  printText: string = '打印';
  setText: string = '设置';
  helpText: string = '帮助';
  aboutText: string = '关于';
  onPrint(name: string): void {}
  onSet(name: string): void {}
  onHelp(name: string): void {}
  onAbout(name: string): void {}
}
const WayToolbar: React.FC<WayToolbarProps> = (props) => {
  const primaryKeys = ['add', 'create'];
  const dangerKeys = ['remove', 'delete'];
  const editKeys = ['update', 'edit'];
  const importKeys = ['ImportData', 'import'];
  const exportKeys = ['ExportData', 'export'];
  function initcommands() {
    const mianButtons: CommandAttribute[] = [];
    const splitButtons: Map<string, CommandAttribute[]> = new Map();
    props.attrs?.forEach((attr) => {
      if (attr.issplit && attr.splitname != undefined) {
        if (splitButtons.has(attr.splitname)) {
          const items = splitButtons.get(attr.splitname);
          items?.push(attr);
        } else {
          splitButtons.set(attr.splitname, [attr]);
        }
      } else {
        mianButtons.push(attr);
      }
    });
    return (
      <Space wrap>
        {mianButtons.map((attr) => {
          if (splitButtons.has(attr.command)) {
            const menu = (
              <Menu>
                {splitButtons.get(attr.command)?.map((sattr, i) => {
                  return (
                    <Menu.Item key={i}>
                      <WayButton
                        key={i}
                        {...attrToButtonProps(sattr)}
                        onClick={() => onClick(sattr.command, sattr)}
                        toolbar={props}
                      />
                    </Menu.Item>
                  );
                })}
              </Menu>
            );
            return (
              <WayButton
                {...attrToButtonProps(attr)}
                icon={<DownOutlined />}
                ismenu={true}
                menu={menu}
                onClick={() => onClick(attr.command, attr)}
                toolbar={props}
              />
            );
          } else {
            return (
              <WayButton
                {...attrToButtonProps(attr)}
                onClick={() => onClick(attr.command, attr)}
                toolbar={props}
              />
            );
          }
        })}
      </Space>
    );
  }
  function getseletrowdisabled(attr: CommandAttribute) {
    if (!props.isselectrow) return false;
    if (props.onCommandDisabled != undefined) return props.onCommandDisabled(attr);
    if (props.selectcount == 1 && attr.isselectrow) return false;
    if (
      props.selectcount != undefined &&
      props.selectcount > 1 &&
      attr.isselectrow &&
      attr.selectmultiple
    )
      return false;
    return attr.isselectrow;
  }
  function inithelps() {
    if (props.helpShow != undefined && props.helpShow != false) {
      const [whp, setWhp] = useState({
        isprint: props.helpShow?.isprint ?? false,
        isset: props.helpShow?.isset ?? false,
        ishelp: props.helpShow?.ishelp ?? false,
        isabout: props.helpShow?.isabout ?? false,
        wvh: props.helpShow?.wvh ?? new DefultViewHelp(),
      });
      return <Space wrap>{showhelp(whp)}</Space>;
    }
  }
  function showhelp(whp: WayHelpProps) {
    const items = [];
    if (whp.isprint) {
      items.push(
        <WayButton
          title={whp.wvh?.printText}
          name={'print'}
          shape={'circle'}
          icon={<PrinterOutlined />}
          onClick={onClick}
        />,
      );
    }
    if (whp.isset) {
      items.push(
        <WayButton
          title={whp.wvh?.setText}
          name={'set'}
          shape={'circle'}
          icon={<SettingOutlined />}
          onClick={onClick}
        />,
      );
    }
    if (whp.ishelp) {
      items.push(
        <WayButton
          title={whp.wvh?.helpText}
          name={'help'}
          shape={'circle'}
          icon={<QuestionOutlined />}
          onClick={onClick}
        />,
      );
    }
    if (whp.isabout) {
      items.push(
        <WayButton
          title={whp.wvh?.aboutText}
          name={'about'}
          shape={'circle'}
          icon={<InfoOutlined />}
          onClick={onClick}
        />,
      );
    }
    if (items.length > 0) {
      items.unshift(<Divider type="vertical" />);
    }
    return items.map((h) => {
      return h;
    });
  }
  function initsearch() {
    if (props.searchShow != undefined && props.searchShow != false) {
      return <WayProSearch {...props.searchShow} />;
    }
  }
  const onClick = (name: any, command: CommandAttribute) => {
    if (props.onClick != undefined) props.onClick(name, command);
  };
  function attrToButtonProps(attr: CommandAttribute) {
    let prop = {
      name: attr.command,
      title: attr.title,
      text: attr.name,
      danger: false,
      block: false,
      type: 'default',
      size: 'default',
      shape: '',
      loading: false,
      icon: '',
      ghost: false,
      disabled: false,
    };
    prop.disabled = getseletrowdisabled(attr) ?? false;
    prop = setTypeAndIcon(prop);
    if (props.iscircle) {
      prop.shape = 'circle';
      prop.title = prop.text;
      prop.text = '';
    }
    return prop;
  }
  function setTypeAndIcon(prop: any) {
    if (primaryKeys.includes(prop.name)) {
      prop.type = 'primary';
      prop.icon = <Icon component={PlusOutlined} />;
    }
    if (dangerKeys.includes(prop.name)) {
      prop.type = 'danger';
      prop.icon = <DeleteOutlined />;
    }
    if (editKeys.includes(prop.name)) {
      prop.icon = <EditOutlined />;
    }
    if (importKeys.includes(prop.name)) {
      prop.icon = <ImportOutlined />;
    }
    if (exportKeys.includes(prop.name)) {
      prop.icon = <ExportOutlined />;
    }
    if (prop.icon == '') {
      prop.icon = <SyncOutlined />;
    }
    return prop;
  }

  const renderDiv = (rows: any[]) => {
    if (rows.length == 3) {
      return (
        <Row gutter={[8, 8]}>
          <Col span={10}>
            <Row justify="start">{rows[0]}</Row>
          </Col>
          <Col span={8}>
            <Row justify="end">{rows[1]}</Row>
          </Col>
          <Col span={6}>
            <Row justify="end">{rows[2]}</Row>
          </Col>
        </Row>
      );
    }
    if (rows.length == 2) {
      return (
        <Row gutter={[8, 8]}>
          <Col span={12}>
            <Row justify="start">{rows[0]}</Row>
          </Col>
          <Col span={12}>
            <Row justify="end">{rows[1]}</Row>
          </Col>
        </Row>
      );
    }
    if (rows.length == 1) {
      return (
        <Row gutter={[8, 8]}>
          <Col span={24}>
            <Row justify="start">{rows[0]}</Row>
          </Col>
        </Row>
      );
    }
    return <></>;
  };
  function render() {
    const itmes = [];
    if (props.searchShow != undefined && props.searchShow != false) {
      itmes.push(<Col>{initsearch()}</Col>);
    }
    if (props.commandShow != undefined && props.commandShow != false) {
      itmes.push(<Col>{initcommands()}</Col>);
    }
    if (props.helpShow != undefined && props.helpShow != false) {
      itmes.push(<Col>{inithelps()}</Col>);
    }
    if (props.isclosecard) {
      return renderDiv(itmes);
    } else {
      return <Card>{renderDiv(itmes)}</Card>;
    }
  }
  return render();
};

export default memo(WayToolbar);
