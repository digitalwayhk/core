import React, { useState, useRef } from 'react';
import { Modal } from 'antd';
import type { ModalProps } from 'antd/lib/modal';

interface DragModalProps extends ModalProps {
  ismove?: boolean;
}

const DragModal: React.FC<DragModalProps> = (props) => {
  const [disabled, setDisabled] = useState(true);
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
        onFocus={() => { }}
        onBlur={() => { }}
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
          <div>{modal}</div>
        )}
      />
    );
  }
  return render();
};
export default DragModal;
