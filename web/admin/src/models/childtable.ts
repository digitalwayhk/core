import { WayModel } from '@/components/WayPlus/waymodel';

const ChildTableModel = {
  namespace: 'childtable',
  ...WayModel({
    initing: async (args) => {
      args.payload = 'childtable';
    },
  }),
};
export default ChildTableModel;
