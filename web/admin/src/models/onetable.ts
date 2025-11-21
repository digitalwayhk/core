import { WayModel } from '@/components/WayPlus/waymodel';

const OneTableModel = {
  namespace: 'onetable',
  ...WayModel({
    initing: async (args) => {
      args.payload = 'onetable';
    },
    searching: async (args) => {
      args.payload.c = 'onetable';
    },
    execing: async (args) => {
      args.payload.c = 'onetable';
    }
  }),
};
export default OneTableModel;
