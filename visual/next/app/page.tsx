'use client';
import { useState, useCallback, useEffect } from 'react';
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
import { layoutFlow } from '../lib/layout_flow'
import SubFlow from '../components/SubFlow';
import 'reactflow/dist/style.css';

const nodeTypes: NodeTypes = {
  SubFlow
};

export function Flow() {
  const { fitView } = useReactFlow();
  const [nodes, setNodes, onNodesChange] = useNodesState([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState([]);
  const [fetchCompleted, setFetchCompleted] = useState(false);

  useEffect(() => {
    fetch('http://localhost:8080').
      then(response => response.json()).
      then(data => {
        setNodes(
          Array.from(data.nodes).map((node: any) => {
            return {
              data: { label: node.name },
              position: { x: 0, y: 0 },
              deletable: false,
              connectable: false,
              extent: 'parent',
              expandParent: true,
              ...node,
            }
          })
        );
        setEdges(
          Array.from(data.edges).map((edge: any) => {
            return {
              animated: true,
              ...edge
            }
          })
        );
        setFetchCompleted(true);
      }).
      catch(error => console.error(error));
  }, []);
  useEffect(() => {
    if (fetchCompleted) {
      onLayout('TB');
    }
  }, [fetchCompleted]);
  const onLayout = useCallback((direction: string) => {
      const layouted = layoutFlow(nodes, edges, direction);

      setNodes([...layouted.nodes]);
      setEdges([...layouted.edges]);

      window.requestAnimationFrame(() => {
        fitView();
      });
    },
    [nodes, edges]
  );

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
      <button onClick={()=>{ onLayout('TB') }}>Refresh</button>
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
