'use client';
import { useEffect, Dispatch } from 'react';
import ReactFlow, {
  Panel,
  Controls,
  Background,
  Node,
  Edge,
  ReactFlowProvider,
  useNodesState,
  useEdgesState,
  useReactFlow,
  NodeTypes,
} from 'reactflow';
import 'reactflow/dist/style.css';
import { layout } from '../lib/layout_flow'
import { initialized_nodes_edges } from '../lib/initialized';
import Step from '../components/Step';

const nodeTypes: NodeTypes = {
  Step,
};

async function refresh({ setNodes, setEdges }: { setNodes: Dispatch<any>, setEdges: Dispatch<any> }) {
  const elkNode = await initialized_nodes_edges('http://localhost:8080/flow');
  const { nodes, edges } = await layout(elkNode);

  setNodes(nodes.map((node: Node) => {
    return {
      className: 'size-fit',
      deletable: false,
      connectable: false,
      extent: node.parentId && 'parent',
      type: 'Step',
      expandParent: true, // TODO: remove
      targetPosition: 'left',
      sourcePosition: 'right',
      ...node,
    }
  }));
  setEdges(edges.map((edge: Edge) => {
    return {
      animated: true,
      deletable: false,
      focusable: false,
      ...edge,
    }
  }));
  return;
}


function Flow() {
  const { fitView } = useReactFlow();
  const [nodes, setNodes, onNodesChange] = useNodesState([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState([]);

  const onRefresh = () => {
    const doRefresh = async () => await refresh({ setNodes, setEdges });
    doRefresh();
    window.requestAnimationFrame(() => fitView());
  };
  useEffect(onRefresh, []);

  return (
    <ReactFlow
      nodes={nodes}
      edges={edges}
      onNodesChange={onNodesChange}
      onEdgesChange={onEdgesChange}
      fitView
      nodeTypes={nodeTypes}
    >
      <Background />
      <Panel position='top-right'>
        <button onClick={onRefresh}>Refresh</button>
      </Panel>
      <Controls />
    </ReactFlow>
  );
};

export default function FlowWithProvider() {
  return (
    <ReactFlowProvider>
      <Flow />
    </ReactFlowProvider>
  );
}
