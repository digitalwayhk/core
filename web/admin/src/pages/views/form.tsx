import { PageContainer } from '@ant-design/pro-layout';


export default (props: any) => {
    console.log(props.match.params);
    return (
        <PageContainer title={props.title}>

        </PageContainer>
      );
  };
