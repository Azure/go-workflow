'use client';
import { ElkNode } from 'elkjs/lib/elk.bundled.js'

let defaultAPIPath = '/flow';

export async function initialized_nodes_edges(apiPath: string): Promise<ElkNode> {
    return fetch(apiPath).
        then(response => response.json()).
        catch(error => {
            console.error(error)
            throw error;
        });
}