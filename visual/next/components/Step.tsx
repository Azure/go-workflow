import { memo } from 'react';
import { Handle, NodeProps, Position, NodeResizer  } from 'reactflow';

function Step({
    data,
    targetPosition,
    sourcePosition,
    isConnectable,
    selected,
}: NodeProps) {
    return (
        <div className='px-2 py-0.5 bg-white rounded-md shadow-lg border-solid border-neutral-200 border-1' style={{height: '100%'}}>
            <NodeResizer color='#94a3b8' isVisible={selected} />
            <Handle className='invisible' type='target' position={targetPosition || Position.Left} isConnectable={isConnectable}></Handle>
            <div className='text-gray-700 text-center'>{data?.label}</div>
            <Handle className='invisible' type='source' position={sourcePosition || Position.Right} isConnectable={isConnectable}></Handle>
        </div>
    );
}

export default memo(Step);