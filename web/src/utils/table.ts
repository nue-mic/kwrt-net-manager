/** 表格分页的统一配置（各页 showTotal 的量词不同，仍由各页自己写）。 */

/** 每页条数选项：终端/日志动辄上千条，给到 2000 可一次看完。 */
export const PAGE_SIZE_OPTIONS = [10, 20, 50, 100, 200, 500, 1000, 2000];

/** 列表页分页默认值：默认 20 条/页，可切换每页条数。 */
export const pageOpts = {
  pageSize: 20,
  showSizeChanger: true,
  pageSizeOptions: PAGE_SIZE_OPTIONS,
};
