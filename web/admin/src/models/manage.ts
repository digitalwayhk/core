import { WayModel } from '@/components/WayPlus/waymodel';

console.log('\ud83d\udce6 [manage.ts] Model 正在加载...');

const ManageModel = {
  namespace: 'manage',
  ...WayModel({
    initing: (args: any) => {
      console.log('\ud83d\udd27 [manage model] initing 钩子被调用:', args);
      console.log('\ud83d\udccd [manage model] payload:', args.payload);
      // 如果需要修改请求路径，可以在这里操作
      // args.payload.c = '/manage/wallets/' + args.payload.c;
    },
    inited: (args: any, result: any) => {
      console.log('\u2705 [manage model] inited 钩子被调用');
      console.log('\ud83d\udcca [manage model] result:', result);
      return result;
    },
    searching: (args: any) => {
      console.log('\ud83d\udd0d [manage model] searching 钩子被调用:', args);
    },
    searched: (args: any, result: any) => {
      console.log('\u2705 [manage model] searched 钩子被调用:', result);
      return result;
    },
    execing: (args: any) => {
      console.log('\u26a1 [manage model] execing 钩子被调用:', args);
    },
    execed: (args: any, result: any) => {
      console.log('\u2705 [manage model] execed 钩子被调用:', result);
      return result;
    },
  }),
};

console.log('\u2705 [manage.ts] Model 已创建:', ManageModel);
console.log('\ud83d\udd11 [manage.ts] namespace:', ManageModel.namespace);
console.log('\ud83d\udee0\ufe0f [manage.ts] effects:', Object.keys(ManageModel.effects || {}));

export default ManageModel;
