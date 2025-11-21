import { GithubOutlined } from '@ant-design/icons';
import { DefaultFooter } from '@ant-design/pro-components';
import React from 'react';

const Footer: React.FC = () => {
  return (
    <DefaultFooter
      style={{
        background: 'none',
      }}
      copyright="Powered by BitZoom"
      links={[
        {
          key: 'BitZoom Exchange Admin',
          title: 'BitZoom Exchange Admin',
          href: 'https://www.riverwa.com',
          blankTarget: true,
        },
        {
          key: 'github',
          title: <GithubOutlined />,
          href: 'https://www.riverwa.com',
          blankTarget: true,
        },
        {
          key: 'BitZoom',
          title: 'BitZoom',
          href: 'https://www.riverwa.com',
          blankTarget: true,
        },
      ]}
    />
  );
};

export default Footer;
