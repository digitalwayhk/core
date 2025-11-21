import React, { useState } from 'react';
import { Tooltip, Button, Modal, Dropdown } from 'antd';
import { WayToolbarProps } from './index';
import { ExclamationCircleOutlined } from '@ant-design/icons';

interface WayButtonProps {
  toolbar?: WayToolbarProps;
  disabled?: boolean;
  ghost?: boolean;
  icon?: string;
  loading?: any; //boolean | { delay: number }
  shape?: string; //circle、 round
  size?: string; //large | middle | small
  target?: any;
  type?: string; //primary | ghost | dashed | danger | link | text
  block?: boolean;
  danger?: boolean;
  text?: string;
  title?: string;
  name?: string;
  key?: string;
  onClick?: any;
  ismenu?: boolean;
  menu?: any;
}

const WayButton: React.FC<WayButtonProps> = (props) => {
  const value = {
    key: props.key ?? props.name,
    name: props.name ?? '',
    title: props.title ?? '',
    text: props.text,
    danger: props.danger ?? false,
    block: props.block ?? false,
    type: props.type ?? 'default',
    target: props.target,
    size: props.size ?? 'default',
    shape: props.shape ?? '',
    icon: props.icon ?? '',
    ghost: props.ghost ?? false,
    disabled: props.disabled ?? false,
  };
  const [load, setLoading] = useState(false);
  function getCommand() {
    if (props.toolbar != undefined && props.name != undefined) {
      if (props.toolbar.attrs != undefined && props.toolbar.attrs.length > 0) {
        return props.toolbar.attrs.find((item) => {
          return item.command == props.name;
        });
      }
    }
    return null;
  }
  const Click = () => {
    if (props.onClick != undefined) {
      try {
        setLoading({ delay: 100 });
        var com = getCommand();
        if (com != null && com.isalert) {
          Modal.confirm({
            title: `您确认要进行${value.text}吗?`,
            icon: <ExclamationCircleOutlined />,
            onOk: () => {
              props.onClick(value);
            },
          });
        } else {
          props.onClick(value);
        }
      } finally {
        setLoading(false);
      }
    }
  };
  function render() {
    if (props.ismenu) {
      return (
        <Dropdown overlay={props.menu}>
          <Tooltip title={value.title}>
            <Button {...value} onClick={Click} loading={load}>
              {value.text}
            </Button>
          </Tooltip>
        </Dropdown>
      );
    } else {
      return (
        <Tooltip title={value.title}>
          <Button {...value} onClick={Click} loading={load}>
            {value.text}
          </Button>
        </Tooltip>
      );
    }
  }
  return render();
};
export default WayButton;
