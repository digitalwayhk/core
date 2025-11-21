import { Card, Divider, Steps } from 'antd';
import type { StepProps } from 'antd/lib/steps';
import React, { useEffect, useState } from 'react';
import WayForm from '.';
import DragModal from './window';

const { Step } = Steps;

interface WayStepFromProps {
  attr?: ModelAttribute;
  title?: string;
  currentStep?: number;
  stepItem?: StepProps[];
  isCard?: boolean;
  isModal?: boolean;
  isShow?: boolean;
  closeOk?: boolean;
  onChange?: (current: number) => void;
  onCurrentStepComponent?: (current: number) => React.ReactDOM;
  onShowChange?: (isshow: boolean) => void;
}

const WayStepFrom: React.FC<WayStepFromProps> = (props) => {
  const [currentStep, setCurrentStep] = useState<number>(props.currentStep ?? 0);
  const [isshow, setModalShow] = useState(props.isShow ?? false);
  useEffect(() => {
    setModalShow(props.isShow ?? false);
  }, [props.isShow]);
  useEffect(() => {
    setCurrentStep(props.currentStep ?? 0);
    if (props.onChange) {
      props.onChange(props.currentStep ?? 0);
    }
  }, [props, props.currentStep]);

  function getCurrentStepAndComponent(current: number) {
    if (props.onCurrentStepComponent) return props.onCurrentStepComponent(current);
    return <WayForm attr={props.attr} ismodal={false} />;
  }
  function renderSteps() {
    return (
      <>
        <Steps
          current={currentStep}
          onChange={(current: number) => {
            setCurrentStep(current);
            if (props.onChange) {
              props.onChange(current);
            }
          }}
        >
          {props.stepItem?.map((item) => {
            // eslint-disable-next-line react/jsx-key
            return <Step {...item} />;
          })}
        </Steps>
        <Divider />
        {getCurrentStepAndComponent(currentStep)}
      </>
    );
  }
  function showChange(show: boolean) {
    setModalShow(show);
    if (props.onShowChange) props.onShowChange(show);
  }
  function render() {
    if (props.isModal) {
      let mprop = {
        onOk: () => {
          showChange(false);
        },
        onCancel: () => {
          showChange(false);
        },
      };
      if (props.closeOk) {
        mprop.footer = false;
      }
      return (
        <DragModal
          maskClosable={false}
          destroyOnClose={true}
          title={props.title}
          width={900}
          visible={isshow}
          {...mprop}
        >
          {renderSteps()}
        </DragModal>
      );
    }
    if (props.isCard)
      return (
        <Card title={props.title} bordered={false}>
          {renderSteps()}
        </Card>
      );
    return renderSteps();
  }
  return render();
};

export default WayStepFrom;
