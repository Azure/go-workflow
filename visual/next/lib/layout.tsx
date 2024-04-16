'use client';
import ELK, { ElkNode } from 'elkjs/lib/elk.bundled.js'
import { Node, Edge } from 'reactflow';
import { fromElk, traverseElk } from './util_elk';

const elk = new ELK();

export async function layoutElk(g: ElkNode): Promise<{ nodes: Node[], edges: Edge[] }> {
  traverseElk({ elkNode: g }, ({ elkNode }) => {
    if (elkNode.children) {
      elkNode.layoutOptions = {
        'elk.algorithm': 'layered',
        'elk.layered.spacing.nodeNodeBetweenLayers': '50',
        'elk.spacing.labelLabel': '5',
        'elk.padding': '[top=40,left=15,bottom=15,right=15]',
        'elk.direction': 'RIGHT',
        ...elkNode.layoutOptions,
      }
    }
  });
  return fromElk(await elk.layout(g));
};
