'use client';
import { useLayoutEffect, useEffect } from 'react';
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
  MarkerType,
  Position,
} from 'reactflow';
import 'reactflow/dist/style.css';
import { layoutElk } from '../lib/layout'
import { toElk } from '../lib/util_elk';
import { initialized_nodes_edges } from '../lib/initialized';
import Step from '../components/Step';
import SubFlow from '../components/SubFlow';

const nodeTypes: NodeTypes = {
  SubFlow,
  Step,
};

function applyConfig({ nodes, edges }: { nodes: Node[], edges: Edge[] }): { layoutNodes: Node[], layoutEdges: Edge[] } {
  let isParent = new Set<string>();
  nodes.forEach((node: Node) => {
    if (node.parentId) {
      isParent.add(node.parentId);
    }
  })
  return {
    layoutNodes: nodes.map((node: Node) => {
      return {
        className: 'px-2 py-0.5 bg-white rounded-lg shadow-lg border-solid border-neutral-200 border-1' + ' ' + (
          isParent.has(node.id) ?
            // Step
            '' :
            // SubFlow
            'max-w-xs'
        ),
        deletable: false,
        connectable: false,
        extent: node.parentId ? 'parent' : undefined,
        type: isParent.has(node.id) ? 'SubFlow' : 'Step',
        expandParent: true, // TODO: remove
        targetPosition: Position.Left,
        sourcePosition: Position.Right,
        ...node,
      }
    }),
    layoutEdges: edges.map((edge: Edge) => {
      return {
        animated: true,
        deletable: false,
        focusable: false,
        markerEnd: { type: MarkerType.ArrowClosed },
        ...edge,
      }
    }),
  }
}

function Flow() {
  const { fitView } = useReactFlow();
  const [nodes, setNodes, onNodesChange] = useNodesState([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState([]);

  const onLayout = () => {
    layoutElk(toElk({ nodes, edges })).then(({ nodes, edges }) => {
      const { layoutNodes, layoutEdges } = applyConfig({ nodes, edges });
      setNodes(layoutNodes)
      setEdges(layoutEdges)
    }).catch((e) => console.error(e));
  };
  const onRefresh = () => {
    const doRefresh = async () => {
      const elkNode = await initialized_nodes_edges('http://localhost:8080/flow')
      const { layoutNodes, layoutEdges } = applyConfig(await layoutElk(elkNode))
      setNodes(layoutNodes)
      setEdges(layoutEdges)
    }
    doRefresh();
    window.requestAnimationFrame(() => fitView());
  };
  useLayoutEffect(onRefresh, []);

  return (
    <ReactFlow
      nodes={nodes}
      edges={edges}
      onNodesChange={onNodesChange}
      onEdgesChange={onEdgesChange}
      fitView
      nodeTypes={nodeTypes}
      maxZoom={10}
      minZoom={0.1}
    >
      <Background />
      <Panel position='top-right'>
        <button className='text-stone-800' onClick={onRefresh}>Refresh</button>
        <p />
        <button className='text-stone-800' onClick={onLayout}>Layout</button>
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
