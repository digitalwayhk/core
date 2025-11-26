import { Pie } from '@ant-design/plots';
import { Card, Segmented, Typography } from 'antd';
import numeral from 'numeral';
import React from 'react';
import type { DataItem } from '../data.d';
import useStyles from '../style.style';

const { Text } = Typography;
const ProportionSales = ({
  dropdownGroup,
  salesType,
  loading,
  salesPieData,
  handleChangeSalesType,
}: {
  loading: boolean;
  dropdownGroup: React.ReactNode;
  salesType: 'all' | 'online' | 'stores'| 'api';
  salesPieData: DataItem[];
  handleChangeSalesType?: (value: 'all' | 'online' | 'stores' | 'api') => void;
}) => {
  const { styles } = useStyles();
  return (
    <Card
      loading={loading}
      className={styles.salesCard}
      variant="borderless"
      title="交易额类别占比"
      style={{
        height: '100%',
      }}
      extra={
        <div className={styles.salesCardExtra}>
          {dropdownGroup}
          <Segmented
            className={styles.salesTypeRadio}
            value={salesType}
            onChange={handleChangeSalesType}
            options={[
              { label: '全部', value: 'all' },
              { label: 'WEB', value: 'online' },
              { label: '手机', value: 'stores' },
              { label: 'API', value: 'api' },
            ]}
            size="middle"
          />
        </div>
      }
    >
      <Text>交易额</Text>
      <Pie
        height={340}
        radius={0.8}
        innerRadius={0.5}
        angleField="y"
        colorField="x"
        data={salesPieData as any}
        legend={false}
        label={{
          position: 'spider',
          text: (item: { x: number; y: number }) =>
            `${item.x}: ${numeral(item.y).format('0,0')}`,
        }}
      />
    </Card>
  );
};
export default ProportionSales;
