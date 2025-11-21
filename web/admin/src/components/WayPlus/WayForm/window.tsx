import React, { useState } from 'react';
import { Modal } from 'antd';
import type { ModalProps } from 'antd/lib/modal';
import Draggable from 'react-draggable';

interface DragModalProps extends ModalProps {
  ismove?: boolean;
}

const DragModal: React.FC<DragModalProps> = (props) => {
  const [bounds, setBounds] = useState({ left: 0, top: 0, bottom: 0, right: 0 });
  const [disabled, setDisabled] = useState(true);
  const draggleRef = React.createRef();

  const onStart = (event, uiData) => {
    const { clientWidth, clientHeight } = window?.document?.documentElement;
    const targetRect = draggleRef?.current?.getBoundingClientRect();
    setBounds({
      left: -targetRect?.left + uiData?.x,
      right: clientWidth - (targetRect?.right - uiData?.x),
      top: -targetRect?.top + uiData?.y,
      bottom: clientHeight - (targetRect?.bottom - uiData?.y),
    });
  };
  function renderTitle() {
    return (
      <div
        style={{
          width: '100%',
          cursor: 'move',
        }}
        onMouseOver={() => {
          if (disabled) {
            setDisabled(false);
          }
        }}
        onMouseOut={() => {
          setDisabled(true);
        }}
        onFocus={() => {}}
        onBlur={() => {}}
      >
        {props.title}
      </div>
    );
  }
  function render() {
    return (
      <Modal
        {...props}
        title={renderTitle()}
        modalRender={(modal) => (
          <Draggable
            disabled={disabled}
            bounds={bounds}
            onStart={(event: any, uiData: any) => onStart(event, uiData)}
          >
            <div ref={draggleRef}>{modal}</div>
          </Draggable>
        )}
      />
    );
  }
  return render();
};
export default DragModal;
